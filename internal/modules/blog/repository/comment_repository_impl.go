package repository

import (
	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
)

type commentRepositoryImpl struct {
	db *gorm.DB
}

// NewCommentRepository creates a new CommentRepository
func NewCommentRepository(db *gorm.DB) CommentRepository {
	return &commentRepositoryImpl{db: db}
}

func (r *commentRepositoryImpl) WithTx(tx *gorm.DB) CommentRepository {
	if tx == nil {
		return r
	}
	return &commentRepositoryImpl{db: tx}
}

func (r *commentRepositoryImpl) Create(comment *domain.Comment) error {
	return r.db.Create(comment).Error
}

func (r *commentRepositoryImpl) Update(comment *domain.Comment) error {
	return r.db.Save(comment).Error
}

func (r *commentRepositoryImpl) Delete(id uuid.UUID) error {
	return r.db.Delete(&domain.Comment{}, id).Error
}

func (r *commentRepositoryImpl) GetByID(id uuid.UUID) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.First(&comment, id).Error
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *commentRepositoryImpl) GetThreaded(postID uuid.UUID) ([]*domain.Comment, error) {
	approvedFilter := func(db *gorm.DB) *gorm.DB {
		return db.Where("status = ?", domain.CommentStatusApproved).Limit(100)
	}

	var comments []*domain.Comment
	err := r.db.
		Where("post_id = ? AND parent_id IS NULL AND status = ?", postID, domain.CommentStatusApproved).
		Preload("Children", approvedFilter).
		Preload("Children.Children", approvedFilter).
		Order("created_at ASC").
		Limit(100).
		Find(&comments).Error
	return comments, err
}

func (r *commentRepositoryImpl) CountByPost(postID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&domain.Comment{}).
		Where("post_id = ? AND status = ?", postID, domain.CommentStatusApproved).
		Count(&count).Error
	return count, err
}

func (r *commentRepositoryImpl) ListPending(offset, limit int) ([]*domain.Comment, int64, error) {
	query := r.db.Model(&domain.Comment{}).Where("status = ?", domain.CommentStatusPending)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var comments []*domain.Comment
	err := query.Order("created_at ASC").Offset(offset).Limit(limit).Find(&comments).Error
	return comments, total, err
}
