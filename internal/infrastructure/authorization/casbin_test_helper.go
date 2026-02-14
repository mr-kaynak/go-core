package authorization

import (
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/mr-kaynak/go-core/internal/core/logger"
)

// NewTestCasbinService creates an in-memory CasbinService for use in tests
// outside the authorization package. No database or config required.
func NewTestCasbinService() (*CasbinService, error) {
	m, err := model.NewModelFromString(getModelText())
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	return &CasbinService{
		enforcer: e,
		logger:   logger.Get().WithFields(logger.Fields{"service": "casbin-test"}),
	}, nil
}
