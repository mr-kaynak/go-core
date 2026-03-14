package service

import (
	"testing"

	"github.com/google/uuid"
)

func TestCategoryService(t *testing.T) {
	_, catRepo := SetupTestEnv()
	slugSvc := NewSlugService()
	svc := NewCategoryService(catRepo, slugSvc)

	t.Run("Create Success", func(t *testing.T) {
		req := &CreateCategoryRequest{
			Name:        "Service Cat",
			Description: "A test cat",
		}
		cat, err := svc.Create(req)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if cat.Slug != "service-cat" {
			t.Errorf("expected slug 'service-cat', got %s", cat.Slug)
		}
	})

	t.Run("Create Conflict", func(t *testing.T) {
		req := &CreateCategoryRequest{Name: "Service Cat"}
		_, err := svc.Create(req)
		if err == nil {
			t.Errorf("expected conflict error")
		}
	})

	t.Run("Update Success and Cycle Detection", func(t *testing.T) {
		req1 := &CreateCategoryRequest{Name: "Cat A"}
		catA, _ := svc.Create(req1)

		req2 := &CreateCategoryRequest{Name: "Cat B", ParentID: ptrString(catA.ID.String())}
		catB, _ := svc.Create(req2)

		req3 := &CreateCategoryRequest{Name: "Cat C", ParentID: ptrString(catB.ID.String())}
		catC, _ := svc.Create(req3)

		// Update B's parent to C (should fail circle)
		updateReq := &UpdateCategoryRequest{ParentID: ptrString(catC.ID.String())}
		_, err := svc.Update(catA.ID, updateReq)
		if err == nil {
			t.Errorf("expected cycle detection error")
		}

		// Valid Update
		validUpdate := &UpdateCategoryRequest{Name: ptrString("Cat A Updated")}
		updatedCatA, err := svc.Update(catA.ID, validUpdate)
		if err != nil || updatedCatA.Slug != "cat-a-updated" {
			t.Errorf("Update failed or slug not updated")
		}
	})

	t.Run("GetByID and Tree", func(t *testing.T) {
		rootReq := &CreateCategoryRequest{Name: "Tree Root"}
		root, _ := svc.Create(rootReq)

		fetched, err := svc.GetByID(root.ID)
		if err != nil || fetched.Name != "Tree Root" {
			t.Errorf("GetByID failed")
		}

		_, err = svc.GetByID(uuid.New())
		if err == nil {
			t.Errorf("expected not found error")
		}

		tree, err := svc.GetTree()
		if err != nil || len(tree) == 0 {
			t.Errorf("GetTree failed")
		}
	})

	t.Run("Delete Rules", func(t *testing.T) {
		parentReq := &CreateCategoryRequest{Name: "Del Parent"}
		parent, _ := svc.Create(parentReq)

		childReq := &CreateCategoryRequest{Name: "Del Child", ParentID: ptrString(parent.ID.String())}
		child, _ := svc.Create(childReq)

		// Cannot delete parent with child
		err := svc.Delete(parent.ID)
		if err == nil {
			t.Errorf("expected error deleting category with child")
		}

		// Can delete child
		err = svc.Delete(child.ID)
		if err != nil {
			t.Errorf("failed deleting child category: %v", err)
		}

		// Can delete parent after child is deleted
		err = svc.Delete(parent.ID)
		if err != nil {
			t.Errorf("failed deleting parent category: %v", err)
		}
	})
}

func ptrString(s string) *string {
	return &s
}
