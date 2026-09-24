package repository

import (
	"errors"
	"fmt"

	"github.com/oralhistory/oralhistory/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordingRepository 录音片段数据访问接口。
type RecordingRepository interface {
	Create(recording *model.Recording) error
	// AppendToQuestion 在事务内把新录音接到问题片段末尾（行锁串行化并发补录）。
	AppendToQuestion(recording *model.Recording) error
	FindByID(id uint) (*model.Recording, error)
	ListByProject(projectID uint) ([]model.Recording, error)
	ListByQuestion(questionID uint) ([]model.Recording, error)
	FindByIDForUpdate(id uint) (*model.Recording, error)
	Update(recording *model.Recording) error
	UpdateStatus(recording *model.Recording) error
	// ReorderByQuestion 按 orderedIDs 重写问题下全部片段的 sort_order；
	// orderedIDs 必须与当前片段集合完全一致，否则回滚并返回 ErrOrderMismatch。
	ReorderByQuestion(questionID uint, orderedIDs []uint) error
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

func (r *recordingRepository) Create(recording *model.Recording) error {
	if err := r.db.Create(recording).Error; err != nil {
		return fmt.Errorf("create recording of question %d: %w", recording.QuestionID, err)
	}
	return nil
}

// AppendToQuestion 锁定问题下的现有片段后，把新录音以 max(sort_order)+1 追加到末尾，
// 保证多次补录并发上传时新片段也稳定落在末尾，且不会插入其他问题。
func (r *recordingRepository) AppendToQuestion(recording *model.Recording) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var maxOrder struct {
			Max int
		}
		// 间隙锁/行锁串行化同一问题上的并发补录。
		if err := tx.Model(&model.Recording{}).
			Where("question_id = ?", recording.QuestionID).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("COALESCE(MAX(sort_order), -1) AS max").
			Scan(&maxOrder).Error; err != nil {
			return fmt.Errorf("lock recordings of question %d before append: %w", recording.QuestionID, err)
		}
		recording.SortOrder = maxOrder.Max + 1
		if err := tx.Create(recording).Error; err != nil {
			return fmt.Errorf("append recording to question %d: %w", recording.QuestionID, err)
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
	// 时间线先按问题顺序分组（片段不混入其他问题），问题内按人工排序播放。
	if err := r.db.Model(&model.Recording{}).
		Where("recordings.project_id = ?", projectID).
		Joins("LEFT JOIN questions ON questions.id = recordings.question_id").
		Order("questions.sort_order ASC, recordings.sort_order ASC, recordings.id ASC").
		Find(&recordings).Error; err != nil {
		return nil, fmt.Errorf("list recordings of project %d: %w", projectID, err)
	}
	return recordings, nil
}

func (r *recordingRepository) ListByQuestion(questionID uint) ([]model.Recording, error) {
	var recordings []model.Recording
	if err := r.db.Where("question_id = ?", questionID).
		Order("sort_order ASC, id ASC").
		Find(&recordings).Error; err != nil {
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

// ReorderByQuestion 在事务内对同一问题的片段重排：
// 校验提交集合与库中集合完全一致（去重、不缺、不串问题），再按提交顺序压缩重写 sort_order 为 0..n-1。
func (r *recordingRepository) ReorderByQuestion(questionID uint, orderedIDs []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var current []model.Recording
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("question_id = ?", questionID).
			Order("sort_order ASC, id ASC").
			Find(&current).Error; err != nil {
			return fmt.Errorf("lock recordings of question %d for reorder: %w", questionID, err)
		}
		currentIDs := make(map[uint]struct{}, len(current))
		for _, rec := range current {
			currentIDs[rec.ID] = struct{}{}
		}
		submitted := make(map[uint]struct{}, len(orderedIDs))
		for _, id := range orderedIDs {
			if _, dup := submitted[id]; dup {
				return ErrOrderMismatch
			}
			if _, ok := currentIDs[id]; !ok {
				return ErrOrderMismatch
			}
			submitted[id] = struct{}{}
		}
		if len(submitted) != len(currentIDs) {
			return ErrOrderMismatch
		}
		for idx, id := range orderedIDs {
			if err := tx.Model(&model.Recording{}).Where("id = ? AND question_id = ?", id, questionID).
				Update("sort_order", idx).Error; err != nil {
				return fmt.Errorf("update sort_order of recording %d in question %d: %w", id, questionID, err)
			}
		}
		return nil
	})
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
