package service

import (
	"log/slog"
	"testing"

	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/dto"
	"github.com/oralhistory/oralhistory/internal/model"
	"github.com/oralhistory/oralhistory/internal/repository"
	"github.com/oralhistory/oralhistory/internal/util"
)

type fakeRecordingRepo struct {
	recordings []model.Recording
	created    *model.Recording
}

func (f *fakeRecordingRepo) CreateWithNextSortOrder(rec *model.Recording) error {
	maxOrder := 0
	for _, r := range f.recordings {
		if r.QuestionID == rec.QuestionID && r.SortOrder > maxOrder {
			maxOrder = r.SortOrder
		}
	}
	rec.SortOrder = maxOrder + 1
	rec.ID = uint(len(f.recordings) + 1)
	f.recordings = append(f.recordings, *rec)
	f.created = rec
	return nil
}

func (f *fakeRecordingRepo) FindByID(id uint) (*model.Recording, error) {
	for i := range f.recordings {
		if f.recordings[i].ID == id {
			return &f.recordings[i], nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeRecordingRepo) ListByProject(projectID uint) ([]model.Recording, error) {
	return nil, nil
}

func (f *fakeRecordingRepo) ListByQuestion(questionID uint) ([]model.Recording, error) {
	var out []model.Recording
	for _, r := range f.recordings {
		if r.QuestionID == questionID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRecordingRepo) FindByIDForUpdate(id uint) (*model.Recording, error) {
	return f.FindByID(id)
}

func (f *fakeRecordingRepo) Update(recording *model.Recording) error { return nil }

func (f *fakeRecordingRepo) UpdateStatus(recording *model.Recording) error { return nil }

// ReorderWithinQuestion 模拟真实仓储行为：校验归属、提交项排前、未提交项顺延。
func (f *fakeRecordingRepo) ReorderWithinQuestion(questionID uint, orderedIDs []uint) ([]model.Recording, error) {
	byID := map[uint]*model.Recording{}
	var current []*model.Recording
	for i := range f.recordings {
		if f.recordings[i].QuestionID == questionID {
			byID[f.recordings[i].ID] = &f.recordings[i]
			current = append(current, &f.recordings[i])
		}
	}
	seen := map[uint]bool{}
	var sequence []*model.Recording
	for _, id := range orderedIDs {
		rec, ok := byID[id]
		if !ok {
			return nil, repository.ErrNotInQuestion
		}
		if !seen[id] {
			seen[id] = true
			sequence = append(sequence, rec)
		}
	}
	for _, rec := range current {
		if !seen[rec.ID] {
			sequence = append(sequence, rec)
		}
	}
	out := make([]model.Recording, 0, len(sequence))
	for pos, rec := range sequence {
		rec.SortOrder = pos + 1
		out = append(out, *rec)
	}
	return out, nil
}

func (f *fakeRecordingRepo) Delete(id uint) error { return nil }

func (f *fakeRecordingRepo) CountByProject(projectID uint) (int64, error) { return 0, nil }

type fakeQuestionRepo struct {
	questions map[uint]*model.Question
}

func (f *fakeQuestionRepo) Create(question *model.Question) error { return nil }
func (f *fakeQuestionRepo) FindByID(id uint) (*model.Question, error) {
	if q, ok := f.questions[id]; ok {
		return q, nil
	}
	return nil, repository.ErrNotFound
}
func (f *fakeQuestionRepo) ListByProject(projectID uint) ([]model.Question, error) {
	return nil, nil
}
func (f *fakeQuestionRepo) Update(question *model.Question) error        { return nil }
func (f *fakeQuestionRepo) Delete(id uint) error                         { return nil }
func (f *fakeQuestionRepo) CountByProject(projectID uint) (int64, error) { return 0, nil }

func newRecordingService(recordings []model.Recording) (RecordingService, *fakeRecordingRepo) {
	recRepo := &fakeRecordingRepo{recordings: recordings}
	projectRepo := &fakeProjectRepo{projects: map[uint]*model.Project{1: {ID: 1, Title: "测试项目"}}}
	questionRepo := &fakeQuestionRepo{questions: map[uint]*model.Question{
		10: {ID: 10, ProjectID: 1, Content: "问题一"},
		20: {ID: 20, ProjectID: 1, Content: "问题二"},
	}}
	return NewRecordingService(recRepo, projectRepo, questionRepo, slog.Default()), recRepo
}

func TestRecordingServiceCreateAppendsToEnd(t *testing.T) {
	actor := &model.User{ID: 1, Username: "interviewer", Role: constants.RoleInterviewer}
	svc, repo := newRecordingService([]model.Recording{
		{ID: 1, ProjectID: 1, QuestionID: 10, SortOrder: 1},
		{ID: 2, ProjectID: 1, QuestionID: 10, SortOrder: 2},
		{ID: 3, ProjectID: 1, QuestionID: 20, SortOrder: 1},
	})
	created, err := svc.Create(actor, &dto.CreateRecordingRequest{ProjectID: 1, QuestionID: 10, DurationSeconds: 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.SortOrder != 3 {
		t.Fatalf("sort_order = %d, want 3（新录音接在该问题末尾）", created.SortOrder)
	}
	if repo.created == nil || repo.created.QuestionID != 10 {
		t.Fatalf("expected recording created under question 10")
	}
	// 其他问题下的录音不受影响
	created2, err := svc.Create(actor, &dto.CreateRecordingRequest{ProjectID: 1, QuestionID: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created2.SortOrder != 2 {
		t.Fatalf("sort_order = %d, want 2（问题 20 内独立递增）", created2.SortOrder)
	}
}

func TestRecordingServiceReorder(t *testing.T) {
	actor := &model.User{ID: 1, Username: "archivist", Role: constants.RoleArchivist}
	seed := func() []model.Recording {
		return []model.Recording{
			{ID: 1, ProjectID: 1, QuestionID: 10, SortOrder: 1},
			{ID: 2, ProjectID: 1, QuestionID: 10, SortOrder: 2},
			{ID: 3, ProjectID: 1, QuestionID: 10, SortOrder: 3},
			{ID: 4, ProjectID: 1, QuestionID: 10, SortOrder: 4},
			{ID: 9, ProjectID: 1, QuestionID: 20, SortOrder: 1},
		}
	}
	cases := []struct {
		name      string
		question  uint
		ids       []uint
		wantOrder []uint // 期望的重排后 ID 顺序
		wantCode  int    // 期望的错误码，0 表示成功
	}{
		{name: "中间片段上移", question: 10, ids: []uint{1, 3, 2, 4}, wantOrder: []uint{1, 3, 2, 4}},
		{name: "末尾片段移到最前", question: 10, ids: []uint{4, 1, 2, 3}, wantOrder: []uint{4, 1, 2, 3}},
		{name: "部分提交时未调整片段顺延", question: 10, ids: []uint{3, 1}, wantOrder: []uint{3, 1, 2, 4}},
		{name: "重复 ID 被拒绝", question: 10, ids: []uint{1, 2, 2}, wantCode: constants.CodeRecordingOrder},
		{name: "混入其他问题的录音被拒绝", question: 10, ids: []uint{1, 9}, wantCode: constants.CodeRecordingOrder},
		{name: "问题不存在", question: 99, ids: []uint{1}, wantCode: constants.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newRecordingService(seed())
			got, err := svc.Reorder(actor, tc.question, tc.ids)
			if tc.wantCode != 0 {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var appErr *util.AppError
				if !asAppError(err, &appErr) {
					t.Fatalf("expected app error, got %v", err)
				}
				if appErr.Code != tc.wantCode {
					t.Fatalf("error code = %d, want %d", appErr.Code, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantOrder) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.wantOrder))
			}
			for i, id := range tc.wantOrder {
				if got[i].ID != id {
					t.Fatalf("position %d = recording %d, want %d", i, got[i].ID, id)
				}
				if got[i].SortOrder != i+1 {
					t.Fatalf("recording %d sort_order = %d, want %d", got[i].ID, got[i].SortOrder, i+1)
				}
			}
		})
	}
}

func TestRecordingServiceReorderKeepsOtherQuestionsUntouched(t *testing.T) {
	actor := &model.User{ID: 1, Username: "archivist", Role: constants.RoleArchivist}
	svc, repo := newRecordingService([]model.Recording{
		{ID: 1, ProjectID: 1, QuestionID: 10, SortOrder: 1},
		{ID: 2, ProjectID: 1, QuestionID: 10, SortOrder: 2},
		{ID: 9, ProjectID: 1, QuestionID: 20, SortOrder: 1},
	})
	if _, err := svc.Reorder(actor, 10, []uint{2, 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range repo.recordings {
		if r.ID == 9 && r.SortOrder != 1 {
			t.Fatalf("问题 20 的录音 sort_order = %d, want 1（不受其他问题排序影响）", r.SortOrder)
		}
	}
}
