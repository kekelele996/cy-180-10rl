package repository

import (
	"errors"
	"fmt"
	"sort"

	"github.com/oralhistory/oralhistory/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordingRepository 录音片段数据访问接口。
type RecordingRepository interface {
	// CreateWithNextSortOrder 在事务内取该问题当前最大 sort_order + 1 后创建（新录音接在末尾）。
	CreateWithNextSortOrder(recording *model.Recording) error
	FindByID(id uint) (*model.Recording, error)
	ListByProject(projectID uint) ([]model.Recording, error)
	ListByQuestion(questionID uint) ([]model.Recording, error)
	FindByIDForUpdate(id uint) (*model.Recording, error)
	Update(recording *model.Recording) error
	UpdateStatus(recording *model.Recording) error
	// ReorderWithinQuestion 在事务内按 orderedIDs 重排某问题下的录音，未提交的录音按原相对顺序顺延
	ReorderWithinQuestion(questionID uint, orderedIDs []uint) ([]model.Recording, error)
	Delete(id uint) error
	CountByProject(projectID uint) (int64, error)
}

type recordingRepository struct {
	db *gorm.DB
}

// NewRecordingRepository 构造录音仓储。
func NewRecordingRepository(db *gorm.DB) RecordingRepository {
	return &recordingRepository{db: db}
}

func (r *recordingRepository) CreateWithNextSortOrder(recording *model.Recording) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var last model.Recording
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("question_id = ?", recording.QuestionID).
			Order("sort_order DESC, id DESC").
			Take(&last).Error
		switch {
		case err == nil:
			recording.SortOrder = last.SortOrder + 1
		case errors.Is(err, gorm.ErrRecordNotFound):
			recording.SortOrder = 1
		default:
			return fmt.Errorf("lock recordings of question %d: %w", recording.QuestionID, err)
		}
		if err := tx.Create(recording).Error; err != nil {
			return fmt.Errorf("create recording of question %d: %w", recording.QuestionID, err)
		}
		return nil
	})
}

func (r *recordingRepository) FindByID(id uint) (*model.Recording, error) {
	var recording model.Recording
	if err := r.db.First(&recording, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find recording by id %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("find recording by id: %w", err)
	}
	return &recording, nil
}

func (r *recordingRepository) ListByProject(projectID uint) ([]model.Recording, error) {
	var recordings []model.Recording
	// 项目时间线：先按问题顺序分组，再按问题内的人工顺序排列，片段不会混入其他问题。
	if err := r.db.
		Joins("JOIN questions ON questions.id = recordings.question_id").
		Where("recordings.project_id = ?", projectID).
		Order("questions.sort_order ASC, recordings.sort_order ASC, recordings.id ASC").
		Find(&recordings).Error; err != nil {
		return nil, fmt.Errorf("list recordings of project %d: %w", projectID, err)
	}
	return recordings, nil
}

func (r *recordingRepository) ListByQuestion(questionID uint) ([]model.Recording, error) {
	var recordings []model.Recording
	if err := r.db.Where("question_id = ?", questionID).Order("sort_order ASC, id ASC").Find(&recordings).Error; err != nil {
		return nil, fmt.Errorf("list recordings of question %d: %w", questionID, err)
	}
	return recordings, nil
}

func (r *recordingRepository) FindByIDForUpdate(id uint) (*model.Recording, error) {
	var recording model.Recording
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&recording, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find recording for update by id %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("find recording for update: %w", err)
	}
	return &recording, nil
}

func (r *recordingRepository) Update(recording *model.Recording) error {
	if err := r.db.Save(recording).Error; err != nil {
		return fmt.Errorf("update recording %d: %w", recording.ID, err)
	}
	return nil
}

func (r *recordingRepository) UpdateStatus(recording *model.Recording) error {
	if err := r.db.Model(recording).Update("status", recording.Status).Error; err != nil {
		return fmt.Errorf("update recording %d status: %w", recording.ID, err)
	}
	return nil
}

// ReorderWithinQuestion 重排单个问题下的录音顺序：orderedIDs 中的录音按提交顺序排前，
// 未提交的录音保持原有相对顺序顺延，跨问题的录音 ID 返回 ErrNotInQuestion。
func (r *recordingRepository) ReorderWithinQuestion(questionID uint, orderedIDs []uint) ([]model.Recording, error) {
	var recordings []model.Recording
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("question_id = ?", questionID).
			Order("sort_order ASC, id ASC").
			Find(&recordings).Error; err != nil {
			return fmt.Errorf("lock recordings of question %d: %w", questionID, err)
		}
		byID := make(map[uint]*model.Recording, len(recordings))
		for i := range recordings {
			byID[recordings[i].ID] = &recordings[i]
		}
		sequence := make([]*model.Recording, 0, len(recordings))
		seen := make(map[uint]bool, len(orderedIDs))
		for _, id := range orderedIDs {
			rec, ok := byID[id]
			if !ok {
				return fmt.Errorf("reorder question %d with recording %d: %w", questionID, id, ErrNotInQuestion)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			sequence = append(sequence, rec)
		}
		for i := range recordings {
			if !seen[recordings[i].ID] {
				sequence = append(sequence, &recordings[i])
			}
		}
		for pos, rec := range sequence {
			if rec.SortOrder == pos+1 {
				continue
			}
			rec.SortOrder = pos + 1
			if err := tx.Model(&model.Recording{}).Where("id = ?", rec.ID).Update("sort_order", rec.SortOrder).Error; err != nil {
				return fmt.Errorf("update sort order of recording %d: %w", rec.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(recordings, func(i, j int) bool {
		if recordings[i].SortOrder != recordings[j].SortOrder {
			return recordings[i].SortOrder < recordings[j].SortOrder
		}
		return recordings[i].ID < recordings[j].ID
	})
	return recordings, nil
}

func (r *recordingRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("recording_id = ?", id).Delete(&model.TimelineMarker{}).Error; err != nil {
			return fmt.Errorf("delete markers of recording %d: %w", id, err)
		}
		if err := tx.Delete(&model.Recording{}, id).Error; err != nil {
			return fmt.Errorf("delete recording %d: %w", id, err)
		}
		return nil
	})
}

func (r *recordingRepository) CountByProject(projectID uint) (int64, error) {
	var total int64
	if err := r.db.Model(&model.Recording{}).Where("project_id = ?", projectID).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count recordings of project %d: %w", projectID, err)
	}
	return total, nil
}
