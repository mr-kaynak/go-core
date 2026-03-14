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
	// Fetch all approved comments for this post in a single query
	var all []*domain.Comment
	err := r.db.
		Where("post_id = ? AND status = ?", postID, domain.CommentStatusApproved).
		Order("created_at ASC").
		Limit(500).
		Find(&all).Error
	if err != nil {
		return nil, err
	}

	// Group comments by parent ID
	childrenOf := make(map[uuid.UUID][]*domain.Comment, len(all))
	for _, c := range all {
		c.Children = nil
		if c.ParentID != nil {
			childrenOf[*c.ParentID] = append(childrenOf[*c.ParentID], c)
		}
	}

	// Recursively build tree so all depth levels are populated
	var buildTree func(comment *domain.Comment)
	buildTree = func(comment *domain.Comment) {
		kids := childrenOf[comment.ID]
		if len(kids) == 0 {
			return
		}
		comment.Children = make([]domain.Comment, len(kids))
		for i, kid := range kids {
			buildTree(kid)
			comment.Children[i] = *kid
		}
	}

	var roots []*domain.Comment
	for _, c := range all {
		if c.ParentID == nil {
			buildTree(c)
			roots = append(roots, c)
		}
	}

	return roots, nil
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
