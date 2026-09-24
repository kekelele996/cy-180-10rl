package service

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/model"
	"github.com/oralhistory/oralhistory/internal/repository"
	"github.com/oralhistory/oralhistory/internal/util"
)

// fakeRecordingRepoForReorder 仅实现 Reorder 相关方法，其余方法沿用接口嵌入的 nil 方法（不会被调用）。
type fakeRecordingRepoForReorder struct {
	repository.RecordingRepository
	recordings map[uint]model.Recording // id -> recording（全部隶属于同一问题）
	reorderErr error
}

func (f *fakeRecordingRepoForReorder) ReorderByQuestion(questionID uint, orderedIDs []uint) error {
	if f.reorderErr != nil {
		return f.reorderErr
	}
	if len(orderedIDs) != len(f.recordings) {
		return repository.ErrOrderMismatch
	}
	seen := map[uint]bool{}
	for idx, id := range orderedIDs {
		if seen[id] {
			return repository.ErrOrderMismatch
		}
		rec, ok := f.recordings[id]
		if !ok || rec.QuestionID != questionID {
			return repository.ErrOrderMismatch
		}
		seen[id] = true
		rec.SortOrder = idx
		f.recordings[id] = rec
	}
	return nil
}

func (f *fakeRecordingRepoForReorder) ListByQuestion(questionID uint) ([]model.Recording, error) {
	out := make([]model.Recording, 0, len(f.recordings))
	for _, rec := range f.recordings {
		if rec.QuestionID == questionID {
			out = append(out, rec)
		}
	}
	// 模拟仓储的 sort_order ASC, id ASC 排序。
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].SortOrder < out[i].SortOrder || (out[j].SortOrder == out[i].SortOrder && out[j].ID < out[i].ID) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

type fakeQuestionRepoForReorder struct {
	repository.QuestionRepository
	exists map[uint]bool
}

func (f *fakeQuestionRepoForReorder) FindByID(id uint) (*model.Question, error) {
	if f.exists[id] {
		return &model.Question{ID: id, ProjectID: 1}, nil
	}
	return nil, repository.ErrNotFound
}

func newReorderService(recordingRepo repository.RecordingRepository, questionRepo repository.QuestionRepository) *recordingService {
	return &recordingService{
		recordingRepo: recordingRepo,
		questionRepo:  questionRepo,
		logger:        slog.Default(),
	}
}

func TestRecordingServiceReorder(t *testing.T) {
	actor := &model.User{ID: 1, Username: "archivist", Role: constants.RoleArchivist}
	base := map[uint]model.Recording{
		10: {ID: 10, QuestionID: 7, SortOrder: 0},
		11: {ID: 11, QuestionID: 7, SortOrder: 1},
		12: {ID: 12, QuestionID: 7, SortOrder: 2},
	}
	questionRepo := &fakeQuestionRepoForReorder{exists: map[uint]bool{7: true}}

	t.Run("保存后的顺序即为收听顺序", func(t *testing.T) {
		recordings := make(map[uint]model.Recording, len(base))
		for k, v := range base {
			recordings[k] = v
		}
		svc := newReorderService(&fakeRecordingRepoForReorder{recordings: recordings}, questionRepo)

		got, err := svc.Reorder(actor, 7, []uint{12, 10, 11})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		wantIDs := []uint{12, 10, 11}
		for i, want := range wantIDs {
			if got[i].ID != want {
				t.Fatalf("position %d = recording %d, want %d", i, got[i].ID, want)
			}
			if got[i].SortOrder != i {
				t.Fatalf("recording %d sort_order = %d, want %d", want, got[i].SortOrder, i)
			}
		}
	})

	t.Run("重复的录音 ID 被拒绝", func(t *testing.T) {
		svc := newReorderService(&fakeRecordingRepoForReorder{recordings: map[uint]model.Recording{}}, questionRepo)
		_, err := svc.Reorder(actor, 7, []uint{10, 10, 11})
		var appErr *util.AppError
		if !asAppError(err, &appErr) || appErr.Code != constants.CodeValidation {
			t.Fatalf("expected validation app error, got %v", err)
		}
	})

	t.Run("问题不存在返回 404 错误码", func(t *testing.T) {
		svc := newReorderService(&fakeRecordingRepoForReorder{recordings: map[uint]model.Recording{}}, questionRepo)
		_, err := svc.Reorder(actor, 999, []uint{10})
		var appErr *util.AppError
		if !asAppError(err, &appErr) || appErr.Code != constants.CodeNotFound {
			t.Fatalf("expected not found app error, got %v", err)
		}
	})

	t.Run("集合不一致（删除中间片段后提交旧顺序）返回冲突", func(t *testing.T) {
		recordings := make(map[uint]model.Recording, len(base))
		for k, v := range base {
			recordings[k] = v
		}
		repo := &fakeRecordingRepoForReorder{recordings: recordings, reorderErr: repository.ErrOrderMismatch}
		svc := newReorderService(repo, questionRepo)
		_, err := svc.Reorder(actor, 7, []uint{10, 11, 12})
		var appErr *util.AppError
		if !asAppError(err, &appErr) || appErr.Code != constants.CodeConflict {
			t.Fatalf("expected conflict app error, got %v", err)
		}
	})
}

func TestRecordingServiceReorderRejectsCrossQuestion(t *testing.T) {
	actor := &model.User{ID: 1, Username: "interviewer", Role: constants.RoleInterviewer}
	// 问题 7 的片段只有 10、11；12 属于问题 8，提交时混入应被仓储拒绝。
	recordingRepo := &fakeRecordingRepoForReorder{
		recordings: map[uint]model.Recording{
			10: {ID: 10, QuestionID: 7},
			11: {ID: 11, QuestionID: 7},
			12: {ID: 12, QuestionID: 8},
		},
		reorderErr: nil,
	}
	questionRepo := &fakeQuestionRepoForReorder{exists: map[uint]bool{7: true}}
	svc := newReorderService(recordingRepo, questionRepo)

	_, err := svc.Reorder(actor, 7, []uint{12, 10})
	if !errors.Is(err, repository.ErrOrderMismatch) {
		t.Fatalf("expected ErrOrderMismatch in chain, got %v", err)
	}
}
