package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type engagementRepositoryImpl struct {
	db *gorm.DB
}

// NewEngagementRepository creates a new EngagementRepository
func NewEngagementRepository(db *gorm.DB) EngagementRepository {
	return &engagementRepositoryImpl{db: db}
}

func (r *engagementRepositoryImpl) WithTx(tx *gorm.DB) EngagementRepository {
	if tx == nil {
		return r
	}
	return &engagementRepositoryImpl{db: tx}
}

// Likes

func (r *engagementRepositoryImpl) CreateLike(ctx context.Context, like *domain.PostLike) error {
	db := r.db.WithContext(ctx)
	return db.Create(like).Error
}

func (r *engagementRepositoryImpl) DeleteLike(ctx context.Context, postID, userID uuid.UUID) error {
	db := r.db.WithContext(ctx)
	return db.Where("post_id = ? AND user_id = ?", postID, userID).Delete(&domain.PostLike{}).Error
}

func (r *engagementRepositoryImpl) IsLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.PostLike{}).Where("post_id = ? AND user_id = ?", postID, userID).Count(&count).Error
	return count > 0, err
}

func (r *engagementRepositoryImpl) ToggleLike(ctx context.Context, postID, userID uuid.UUID) (bool, error) {
	db := r.db.WithContext(ctx)
	var liked bool
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing domain.PostLike
		findErr := tx.Where("post_id = ? AND user_id = ?", postID, userID).
			First(&existing).Error

		if findErr == nil {
			// Like exists — delete it (unlike)
			liked = false
			return tx.Delete(&existing).Error
		}

		if findErr != gorm.ErrRecordNotFound {
			return findErr
		}

		// No like exists — create one
		like := &domain.PostLike{PostID: postID, UserID: userID}
		if createErr := tx.Create(like).Error; createErr != nil {
			if isUniqueViolation(createErr) {
				liked = true
				return nil
			}
			return createErr
		}
		liked = true
		return nil
	})
	return liked, err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "23505")
}

// Views

func (r *engagementRepositoryImpl) CreateView(ctx context.Context, view *domain.PostView) error {
	db := r.db.WithContext(ctx)
	return db.Create(view).Error
}

func (r *engagementRepositoryImpl) HasRecentView(
	ctx context.Context, postID uuid.UUID, ip string, since time.Time,
) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.PostView{}).
		Where("post_id = ? AND ip_address = ? AND viewed_at > ?", postID, ip, since).
		Count(&count).Error
	return count > 0, err
}

func (r *engagementRepositoryImpl) HasRecentUserView(
	ctx context.Context, postID, userID uuid.UUID, since time.Time,
) (bool, error) {
	db := r.db.WithContext(ctx)
	var count int64
	err := db.Model(&domain.PostView{}).
		Where("post_id = ? AND user_id = ? AND viewed_at > ?", postID, userID, since).
		Count(&count).Error
	return count > 0, err
}

// Shares

func (r *engagementRepositoryImpl) CreateShare(ctx context.Context, share *domain.PostShare) error {
	db := r.db.WithContext(ctx)
	return db.Create(share).Error
}

// Stats

func (r *engagementRepositoryImpl) GetStats(ctx context.Context, postID uuid.UUID) (*domain.PostStats, error) {
	db := r.db.WithContext(ctx)
	var stats domain.PostStats
	err := db.Where("post_id = ?", postID).First(&stats).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.PostStats{PostID: postID}, nil
		}
		return nil, err
	}
	return &stats, nil
}

func (r *engagementRepositoryImpl) GetBatchStats(ctx context.Context, postIDs []uuid.UUID) ([]*domain.PostStats, error) {
	db := r.db.WithContext(ctx)
	var stats []*domain.PostStats
	err := db.Where("post_id IN ?", postIDs).Find(&stats).Error
	return stats, err
}

func (r *engagementRepositoryImpl) UpsertStats(ctx context.Context, stats *domain.PostStats) error {
	db := r.db.WithContext(ctx)
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "post_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"like_count", "view_count", "share_count", "comment_count", "updated_at"}),
	}).Create(stats).Error
}

func (r *engagementRepositoryImpl) IncrementStat(ctx context.Context, postID uuid.UUID, field string, delta int) error {
	db := r.db.WithContext(ctx)
	allowed := map[string]bool{
		"like_count": true, "view_count": true,
		"share_count": true, "comment_count": true,
	}
	if !allowed[field] {
		return fmt.Errorf("invalid stat field: %s", field)
	}

	return db.Exec(
		fmt.Sprintf(`INSERT INTO blog_post_stats (post_id, %[1]s, updated_at)
			VALUES (?, MAX(?, 0), CURRENT_TIMESTAMP)
			ON CONFLICT (post_id) DO UPDATE
			SET %[1]s = MAX(blog_post_stats.%[1]s + EXCLUDED.%[1]s, 0),
			    updated_at = CURRENT_TIMESTAMP`, field),
		postID, delta,
	).Error
}

// Trending & Popular

func (r *engagementRepositoryImpl) GetTrending(ctx context.Context, query TrendingQuery) ([]*domain.TrendingPost, error) {
	db := r.db.WithContext(ctx)
	since := time.Now().AddDate(0, 0, -query.Days)

	var posts []*domain.TrendingPost
	err := db.Table("blog_posts").
		Select("blog_posts.*, "+
			"(COALESCE(s.view_count,0) * ? + COALESCE(s.like_count,0) * ? + "+
			"COALESCE(s.comment_count,0) * ? + COALESCE(s.share_count,0) * ?) as trending_score",
			query.ViewWeight, query.LikeWeight, query.CommentWeight, query.ShareWeight).
		Joins("LEFT JOIN blog_post_stats s ON s.post_id = blog_posts.id").
		Where("blog_posts.status = ? AND blog_posts.published_at > ? AND blog_posts.deleted_at IS NULL",
			domain.PostStatusPublished, since).
		Order("trending_score DESC").
		Preload("Category").Preload("Tags").Preload("Stats").
		Limit(query.Limit).
		Find(&posts).Error
	return posts, err
}

func (r *engagementRepositoryImpl) GetPopular(ctx context.Context, limit int) ([]*domain.Post, error) {
	db := r.db.WithContext(ctx)
	var posts []*domain.Post
	err := db.
		Joins("LEFT JOIN blog_post_stats s ON s.post_id = blog_posts.id").
		Where("blog_posts.status = ? AND blog_posts.deleted_at IS NULL", domain.PostStatusPublished).
		Order("COALESCE(s.view_count, 0) + COALESCE(s.like_count, 0) * 3 DESC").
		Preload("Category").Preload("Tags").Preload("Stats").
		Limit(limit).
		Find(&posts).Error
	return posts, err
}
