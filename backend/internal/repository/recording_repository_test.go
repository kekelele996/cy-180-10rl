package repository

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestRecordingRepositoryReorderMismatch 提交集合与库中集合不一致时，事务回滚且不产生 UPDATE。
func TestRecordingRepositoryReorderMismatch(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `recordings` WHERE question_id = ?")).
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"id", "question_id", "sort_order"}).
			AddRow(10, 7, 0).
			AddRow(11, 7, 1))
	mock.ExpectRollback()

	err := repo.ReorderByQuestion(7, []uint{10, 12})
	if !errors.Is(err, ErrOrderMismatch) {
		t.Fatalf("expected ErrOrderMismatch, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestRecordingRepositoryReorderSuccess 集合一致时按提交顺序压缩重写 sort_order 为 0..n-1。
func TestRecordingRepositoryReorderSuccess(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `recordings` WHERE question_id = ?")).
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{"id", "question_id", "sort_order"}).
			AddRow(10, 7, 0).
			AddRow(11, 7, 1).
			AddRow(12, 7, 2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `recordings` SET `sort_order`=?,`updated_at`=? WHERE id = ? AND question_id = ?")).
		WithArgs(0, sqlmock.AnyArg(), 12, 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `recordings` SET `sort_order`=?,`updated_at`=? WHERE id = ? AND question_id = ?")).
		WithArgs(1, sqlmock.AnyArg(), 10, 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `recordings` SET `sort_order`=?,`updated_at`=? WHERE id = ? AND question_id = ?")).
		WithArgs(2, sqlmock.AnyArg(), 11, 7).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.ReorderByQuestion(7, []uint{12, 10, 11}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

// TestRecordingRepositoryListByQuestionOrder 问题下列表按 sort_order ASC, id ASC 返回。
func TestRecordingRepositoryListByQuestionOrder(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRecordingRepository(db)

	rows := sqlmock.NewRows([]string{"id", "question_id", "sort_order"}).
		AddRow(12, 7, 0).
		AddRow(10, 7, 1).
		AddRow(11, 7, 2)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `recordings` WHERE question_id = ?")).
		WithArgs(7).
		WillReturnRows(rows)

	got, err := repo.ListByQuestion(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []uint{12, 10, 11}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("position %d = %d, want %d", i, got[i].ID, id)
		}
	}
}
