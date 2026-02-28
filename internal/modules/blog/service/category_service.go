package service

import (
	stderrors "errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/mr-kaynak/go-core/internal/core/errors"
	"github.com/mr-kaynak/go-core/internal/core/logger"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"github.com/mr-kaynak/go-core/internal/modules/blog/repository"
	"gorm.io/gorm"
)

// CreateCategoryRequest holds the request data for creating a category
type CreateCategoryRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=100"`
	Description string  `json:"description" validate:"max=500"`
	ParentID    *string `json:"parent_id" validate:"omitempty,uuid"`
	SortOrder   int     `json:"sort_order"`
}

// UpdateCategoryRequest holds the request data for updating a category
type UpdateCategoryRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=1,max=100"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	ParentID    *string `json:"parent_id" validate:"omitempty,uuid"`
	SortOrder   *int    `json:"sort_order"`
}

// CategoryService handles blog category business logic
type CategoryService struct {
	categoryRepo repository.CategoryRepository
	slugSvc      *SlugService
	logger       *logger.Logger
}

// NewCategoryService creates a new CategoryService
func NewCategoryService(categoryRepo repository.CategoryRepository, slugSvc *SlugService) *CategoryService {
	return &CategoryService{
		categoryRepo: categoryRepo,
		slugSvc:      slugSvc,
		logger:       logger.Get().WithFields(logger.Fields{"service": "blog_category"}),
	}
}

// Create creates a new category
func (s *CategoryService) Create(req *CreateCategoryRequest) (*domain.Category, error) {
	slug := s.slugSvc.Generate(req.Name)
	exists, err := s.categoryRepo.ExistsBySlug(slug)
	if err != nil {
		return nil, errors.NewInternalError("Failed to check slug")
	}
	if exists {
		return nil, errors.New(errors.CodeBlogSlugConflict, http.StatusConflict, "Slug Conflict", "Category with this slug already exists")
	}

	var parentID *uuid.UUID
	if req.ParentID != nil {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			return nil, errors.NewBadRequest("Invalid parent ID format")
		}
		parentID = &pid
	}

	category := &domain.Category{
		Name:        req.Name,
		Slug:        slug,
		Description: req.Description,
		ParentID:    parentID,
		SortOrder:   req.SortOrder,
	}

	if err := s.categoryRepo.Create(category); err != nil {
		s.logger.Error("Failed to create category", "error", err)
		return nil, errors.NewInternalError("Failed to create category")
	}

	s.logger.Info("Category created", "category_id", category.ID)
	return category, nil
}

// Update updates an existing category
func (s *CategoryService) Update(id uuid.UUID, req *UpdateCategoryRequest) (*domain.Category, error) {
	category, err := s.categoryRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogCategoryNotFound, http.StatusNotFound, "Category Not Found", "Category not found")
		}
		return nil, errors.NewInternalError("Failed to get category")
	}

	if req.Name != nil {
		category.Name = *req.Name
		newSlug := s.slugSvc.Generate(*req.Name)
		exists, err := s.categoryRepo.ExistsBySlugExcluding(newSlug, id)
		if err != nil {
			return nil, errors.NewInternalError("Failed to check slug")
		}
		if exists {
			return nil, errors.New(errors.CodeBlogSlugConflict, http.StatusConflict, "Slug Conflict", "Category with this slug already exists")
		}
		category.Slug = newSlug
	}
	if req.Description != nil {
		category.Description = *req.Description
	}
	if req.ParentID != nil {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			return nil, errors.NewBadRequest("Invalid parent ID format")
		}
		if pid == id {
			return nil, errors.NewBadRequest("Category cannot be its own parent")
		}
		// Detect indirect cycles: walk parent chain to ensure no cycle
		if err := s.detectCycle(id, pid); err != nil {
			return nil, err
		}
		category.ParentID = &pid
	}
	if req.SortOrder != nil {
		category.SortOrder = *req.SortOrder
	}

	if err := s.categoryRepo.Update(category); err != nil {
		return nil, errors.NewInternalError("Failed to update category")
	}

	s.logger.Info("Category updated", "category_id", id)
	return category, nil
}

// Delete deletes a category
func (s *CategoryService) Delete(id uuid.UUID) error {
	if err := s.categoryRepo.Delete(id); err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(errors.CodeBlogCategoryNotFound, http.StatusNotFound, "Category Not Found", "Category not found")
		}
		return errors.NewInternalError("Failed to delete category")
	}
	s.logger.Info("Category deleted", "category_id", id)
	return nil
}

// GetTree returns all categories in a tree structure
func (s *CategoryService) GetTree() ([]*domain.Category, error) {
	return s.categoryRepo.GetTree()
}

// detectCycle walks the parent chain from candidateParentID to detect if setting
// it as parent of categoryID would create a cycle (e.g., A->B->C->A).
func (s *CategoryService) detectCycle(categoryID, candidateParentID uuid.UUID) error {
	visited := map[uuid.UUID]bool{categoryID: true}
	current := candidateParentID

	for i := 0; i < 100; i++ { // depth limit to prevent infinite loop
		if visited[current] {
			return errors.NewBadRequest("Circular parent reference detected")
		}
		visited[current] = true

		cat, err := s.categoryRepo.GetByID(current)
		if err != nil {
			return nil // parent doesn't exist or DB error — will be caught by FK constraint
		}
		if cat.ParentID == nil {
			return nil // reached root, no cycle
		}
		current = *cat.ParentID
	}
	return errors.NewBadRequest("Category hierarchy too deep")
}

// GetByID returns a category by ID
func (s *CategoryService) GetByID(id uuid.UUID) (*domain.Category, error) {
	cat, err := s.categoryRepo.GetByID(id)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(errors.CodeBlogCategoryNotFound, http.StatusNotFound, "Category Not Found", "Category not found")
		}
		return nil, errors.NewInternalError("Failed to get category")
	}
	return cat, nil
}
