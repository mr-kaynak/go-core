package validation

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/mr-kaynak/go-core/internal/core/errors"
)

// Validator wraps the validator instance
type Validator struct {
	validator *validator.Validate
}

// New creates a new validator instance
func New() *Validator {
	v := validator.New()

	// Register custom validators
	registerCustomValidators(v)

	// Use JSON tag names in error messages
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	return &Validator{
		validator: v,
	}
}

// ValidateStruct validates a struct and returns a ProblemDetail error if validation fails
func (v *Validator) ValidateStruct(s interface{}) error {
	if err := v.validator.Struct(s); err != nil {
		return v.formatValidationError(err)
	}
	return nil
}

// ValidateVar validates a single variable
func (v *Validator) ValidateVar(field interface{}, tag string) error {
	if err := v.validator.Var(field, tag); err != nil {
		return v.formatValidationError(err)
	}
	return nil
}

// formatValidationError formats validation errors into ProblemDetail
func (v *Validator) formatValidationError(err error) error {
	var errorMessages []string
	var errorFields map[string]interface{}

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		errorFields = make(map[string]interface{})

		for _, e := range validationErrors {
			field := e.Field()
			tag := e.Tag()
			param := e.Param()

			msg := v.getErrorMessage(field, tag, param)
			errorMessages = append(errorMessages, msg)
			errorFields[field] = map[string]interface{}{
				"tag":   tag,
				"value": e.Value(),
				"error": msg,
			}
		}
	} else {
		errorMessages = append(errorMessages, err.Error())
	}

	problemDetail := errors.NewValidationError(strings.Join(errorMessages, "; "))
	if errorFields != nil {
		_ = problemDetail.WithMeta("fields", errorFields)
	}

	return problemDetail
}

// getErrorMessage returns a human-readable error message for validation errors
//
//nolint:gocyclo // error message mapping requires many cases
func (v *Validator) getErrorMessage(field, tag, param string) string {
	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters long", field, param)
	case "max":
		return fmt.Sprintf("%s must be at most %s characters long", field, param)
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, param)
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, param)
	case "alpha":
		return fmt.Sprintf("%s must contain only alphabetic characters", field)
	case "alphanum":
		return fmt.Sprintf("%s must contain only alphanumeric characters", field)
	case "numeric":
		return fmt.Sprintf("%s must be a valid number", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", field)
	case "datetime":
		return fmt.Sprintf("%s must be a valid datetime", field)
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, param)
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters long", field, param)
	case "eq":
		return fmt.Sprintf("%s must be equal to %s", field, param)
	case "ne":
		return fmt.Sprintf("%s must not be equal to %s", field, param)
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, param)
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, param)
	case "password":
		return fmt.Sprintf("%s must be at least 8 characters with uppercase, lowercase, number, and special character", field)
	case "username":
		return fmt.Sprintf("%s must be 3-20 characters and can only contain letters, numbers, and underscores", field)
	case "phone":
		return fmt.Sprintf("%s must be a valid phone number", field)
	default:
		return fmt.Sprintf("%s failed %s validation", field, tag)
	}
}

// registerCustomValidators registers custom validation tags
//
//nolint:gocyclo // registering validators requires many registrations
func registerCustomValidators(v *validator.Validate) {
	// Password validator: min 8 chars, 1 uppercase, 1 lowercase, 1 number, 1 special char
	_ = v.RegisterValidation("password", func(fl validator.FieldLevel) bool {
		password := fl.Field().String()
		if len(password) < 8 {
			return false
		}

		var (
			hasUpper   bool
			hasLower   bool
			hasNumber  bool
			hasSpecial bool
		)

		for _, char := range password {
			switch {
			case 'A' <= char && char <= 'Z':
				hasUpper = true
			case 'a' <= char && char <= 'z':
				hasLower = true
			case '0' <= char && char <= '9':
				hasNumber = true
			case strings.ContainsRune("!@#$%^&*()_+-=[]{}|;:,.<>?", char):
				hasSpecial = true
			}
		}

		return hasUpper && hasLower && hasNumber && hasSpecial
	})

	// Username validator: 3-20 chars, alphanumeric and underscore only
	_ = v.RegisterValidation("username", func(fl validator.FieldLevel) bool {
		username := fl.Field().String()
		if len(username) < 3 || len(username) > 20 {
			return false
		}

		for _, char := range username {
			if (char < 'a' || char > 'z') &&
				(char < 'A' || char > 'Z') &&
				(char < '0' || char > '9') &&
				char != '_' {
				return false
			}
		}

		return true
	})

	// Phone number validator (basic international format)
	_ = v.RegisterValidation("phone", func(fl validator.FieldLevel) bool {
		phone := fl.Field().String()
		// Remove spaces and dashes
		phone = strings.ReplaceAll(phone, " ", "")
		phone = strings.ReplaceAll(phone, "-", "")

		// Check if it starts with + and contains only digits
		phone = strings.TrimPrefix(phone, "+")

		if len(phone) < 10 || len(phone) > 15 {
			return false
		}

		for _, char := range phone {
			if char < '0' || char > '9' {
				return false
			}
		}

		return true
	})
}

// ValidationRules contains common validation rules
type ValidationRules struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,password"` //nolint:gosec // G117: validation DTO field, not a credential
	Username string `json:"username" validate:"required,username"`
	Phone    string `json:"phone" validate:"required,phone"`
}

// Default validator instance
var (
	defaultValidator *Validator
	validatorOnce    sync.Once
)

// Init initializes the default validator
func Init() {
	validatorOnce.Do(func() {
		defaultValidator = New()
	})
}

// Struct validates a struct using the default validator
func Struct(s interface{}) error {
	Init()
	return defaultValidator.ValidateStruct(s)
}

// Var validates a variable using the default validator
func Var(field interface{}, tag string) error {
	Init()
	return defaultValidator.ValidateVar(field, tag)
}
