package repository

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/oralhistory/oralhistory/internal/model"
)

var recordingColumns = []string{
	"id", "project_id", "question_id", "audio_key", "duration_seconds",
	"summary", "status", "sort_order", "created_by", "created_at", "updated_at",
}

func TestRecordingRepositoryListByProjectOrderedByQuestionAndSortOrder(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	// 项目时间线必须按「问题顺序 → 问题内人工顺序」排列，片段不混入其他问题。
	mock.ExpectQuery(regexp.QuoteMeta(
		"JOIN questions ON questions.id = recordings.question_id " +
			"WHERE recordings.project_id = ? " +
			"ORDER BY questions.sort_order ASC, recordings.sort_order ASC, recordings.id ASC")).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows(recordingColumns).
			AddRow(1, 1, 10, "", 30, "", "ready", 1, 1, time.Now(), time.Now()).
			AddRow(2, 1, 10, "", 30, "", "ready", 2, 1, time.Now(), time.Now()))

	recordings, err := repo.ListByProject(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recordings) != 2 {
		t.Fatalf("len = %d, want 2", len(recordings))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRecordingRepositoryCreateAppendsNextSortOrder(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	mock.ExpectBegin()
	// 事务内 SELECT ... FOR UPDATE 取该问题当前最大序号。
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT * FROM `recordings` WHERE question_id = ? ORDER BY sort_order DESC, id DESC LIMIT ? FOR UPDATE")).
		WithArgs(10, 1).
		WillReturnRows(sqlmock.NewRows(recordingColumns).
			AddRow(5, 1, 10, "", 30, "", "ready", 2, 1, time.Now(), time.Now()))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `recordings`")).
		WithArgs(1, 10, "", 30, "", "recording", 3, 1, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(6, 1))
	mock.ExpectCommit()

	rec := &model.Recording{ProjectID: 1, QuestionID: 10, DurationSeconds: 30, Status: "recording", CreatedBy: 1}
	if err := repo.CreateWithNextSortOrder(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.SortOrder != 3 {
		t.Fatalf("sort_order = %d, want 3（新录音接在末尾）", rec.SortOrder)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRecordingRepositoryReorderWithinQuestion(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	mock.ExpectBegin()
	// 事务内锁定该问题全部录音。
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT * FROM `recordings` WHERE question_id = ? ORDER BY sort_order ASC, id ASC FOR UPDATE")).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows(recordingColumns).
			AddRow(1, 1, 10, "", 30, "", "ready", 1, 1, time.Now(), time.Now()).
			AddRow(2, 1, 10, "", 30, "", "ready", 2, 1, time.Now(), time.Now()).
			AddRow(3, 1, 10, "", 30, "", "ready", 3, 1, time.Now(), time.Now()))
	// 新顺序 [2,1,3]：rec2 → 1，rec1 → 2，rec3 序号不变不更新。
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `recordings` SET `sort_order`=?,`updated_at`=? WHERE id = ?")).
		WithArgs(1, sqlmock.AnyArg(), 2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `recordings` SET `sort_order`=?,`updated_at`=? WHERE id = ?")).
		WithArgs(2, sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repo.ReorderWithinQuestion(10, []uint{2, 1, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantIDs := []uint{2, 1, 3}
	for i, id := range wantIDs {
		if got[i].ID != id || got[i].SortOrder != i+1 {
			t.Fatalf("position %d = (id=%d, sort_order=%d), want (id=%d, sort_order=%d)",
				i, got[i].ID, got[i].SortOrder, id, i+1)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRecordingRepositoryReorderRejectsForeignRecording(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT * FROM `recordings` WHERE question_id = ? ORDER BY sort_order ASC, id ASC FOR UPDATE")).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows(recordingColumns).
			AddRow(1, 1, 10, "", 30, "", "ready", 1, 1, time.Now(), time.Now()))
	mock.ExpectRollback()

	// 录音 9 属于其他问题，排序必须被拒绝并回滚，不能跨问题混排。
	if _, err := repo.ReorderWithinQuestion(10, []uint{1, 9}); !errors.Is(err, ErrNotInQuestion) {
		t.Fatalf("expected ErrNotInQuestion, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
