package validation

import (
	"testing"

	"github.com/mr-kaynak/go-core/internal/core/errors"
)

// TestValidateStructValid tests struct validation with valid data
func TestValidateStructValid(t *testing.T) {
	type ValidStruct struct {
		Username string `json:"username" validate:"required,min=3,max=32"`
		Email    string `json:"email" validate:"required,email"`
		Age      int    `json:"age" validate:"required,min=0,max=150"`
	}

	validator := New()

	tests := []struct {
		name  string
		input ValidStruct
	}{
		{
			name: "valid struct",
			input: ValidStruct{
				Username: "johndoe",
				Email:    "john@example.com",
				Age:      25,
			},
		},
		{
			name: "minimum length",
			input: ValidStruct{
				Username: "abc",
				Email:    "a@b.com",
				Age:      1,
			},
		},
		{
			name: "maximum length",
			input: ValidStruct{
				Username: "abcdefghijklmnopqrstuvwxyz123456",
				Email:    "test@example.com",
				Age:      150,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(tt.input)
			if err != nil {
				t.Errorf("expected valid struct, got error: %v", err)
			}
		})
	}
}

// TestValidateStructInvalid tests struct validation with invalid data
func TestValidateStructInvalid(t *testing.T) {
	type InvalidStruct struct {
		Username string `json:"username" validate:"required,min=3,max=32"`
		Email    string `json:"email" validate:"required,email"`
		Age      int    `json:"age" validate:"required,min=0,max=150"`
	}

	validator := New()

	tests := []struct {
		name  string
		input InvalidStruct
	}{
		{
			name: "empty username",
			input: InvalidStruct{
				Username: "",
				Email:    "john@example.com",
				Age:      25,
			},
		},
		{
			name: "username too short",
			input: InvalidStruct{
				Username: "ab",
				Email:    "john@example.com",
				Age:      25,
			},
		},
		{
			name: "username too long",
			input: InvalidStruct{
				Username: "abcdefghijklmnopqrstuvwxyz1234567",
				Email:    "john@example.com",
				Age:      25,
			},
		},
		{
			name: "invalid email format",
			input: InvalidStruct{
				Username: "johndoe",
				Email:    "notanemail",
				Age:      25,
			},
		},
		{
			name: "age below minimum",
			input: InvalidStruct{
				Username: "johndoe",
				Email:    "john@example.com",
				Age:      -1,
			},
		},
		{
			name: "age above maximum",
			input: InvalidStruct{
				Username: "johndoe",
				Email:    "john@example.com",
				Age:      151,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(tt.input)
			if err == nil {
				t.Errorf("expected validation error, but validation passed")
			}

			// Verify it's a problem detail error
			pd := errors.GetProblemDetail(err)
			if pd == nil {
				t.Errorf("expected ProblemDetail error")
			}
		})
	}
}

// TestValidateVarValid tests variable validation with valid data
func TestValidateVarValid(t *testing.T) {
	validator := New()

	tests := []struct {
		name  string
		value interface{}
		tag   string
	}{
		{
			name:  "valid email",
			value: "test@example.com",
			tag:   "email",
		},
		{
			name:  "valid numeric string",
			value: "12345",
			tag:   "numeric",
		},
		{
			name:  "valid min length",
			value: "hello",
			tag:   "min=3",
		},
		{
			name:  "valid max length",
			value: "test",
			tag:   "max=10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateVar(tt.value, tt.tag)
			if err != nil {
				t.Errorf("expected valid value, got error: %v", err)
			}
		})
	}
}

// TestValidateVarInvalid tests variable validation with invalid data
func TestValidateVarInvalid(t *testing.T) {
	validator := New()

	tests := []struct {
		name  string
		value interface{}
		tag   string
	}{
		{
			name:  "invalid email",
			value: "notanemail",
			tag:   "email",
		},
		{
			name:  "invalid numeric",
			value: "abc123",
			tag:   "numeric",
		},
		{
			name:  "value below min length",
			value: "ab",
			tag:   "min=3",
		},
		{
			name:  "value above max length",
			value: "toolongstring",
			tag:   "max=5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateVar(tt.value, tt.tag)
			if err == nil {
				t.Errorf("expected validation error, but validation passed")
			}
		})
	}
}

// TestValidateEmail tests email validation specifically
func TestValidateEmail(t *testing.T) {
	validator := New()

	tests := []struct {
		name  string
		email string
		valid bool
	}{
		{
			name:  "valid simple email",
			email: "test@example.com",
			valid: true,
		},
		{
			name:  "valid complex email",
			email: "user.name+tag@example.co.uk",
			valid: true,
		},
		{
			name:  "invalid no domain",
			email: "test@",
			valid: false,
		},
		{
			name:  "invalid no local part",
			email: "@example.com",
			valid: false,
		},
		{
			name:  "invalid no @",
			email: "testexample.com",
			valid: false,
		},
		{
			name:  "invalid spaces",
			email: "test @example.com",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateVar(tt.email, "email")
			if tt.valid && err != nil {
				t.Errorf("expected valid email %q, got error: %v", tt.email, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected invalid email %q, but validation passed", tt.email)
			}
		})
	}
}

// TestValidateRequired tests required field validation
func TestValidateRequired(t *testing.T) {
	type RequiredFieldStruct struct {
		Name  string `json:"name" validate:"required"`
		Email string `json:"email" validate:"required"`
	}

	validator := New()

	tests := []struct {
		name  string
		input RequiredFieldStruct
		valid bool
	}{
		{
			name: "all fields present",
			input: RequiredFieldStruct{
				Name:  "John",
				Email: "john@example.com",
			},
			valid: true,
		},
		{
			name: "name missing",
			input: RequiredFieldStruct{
				Name:  "",
				Email: "john@example.com",
			},
			valid: false,
		},
		{
			name: "email missing",
			input: RequiredFieldStruct{
				Name:  "John",
				Email: "",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(tt.input)
			if tt.valid && err != nil {
				t.Errorf("expected valid struct, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected validation error, but validation passed")
			}
		})
	}
}

// TestValidateLengthConstraints tests min/max length validation
func TestValidateLengthConstraints(t *testing.T) {
	type LengthStruct struct {
		ShortField string `json:"short_field" validate:"min=1,max=5"`
		LongField  string `json:"long_field" validate:"min=10,max=100"`
	}

	validator := New()

	tests := []struct {
		name  string
		input LengthStruct
		valid bool
	}{
		{
			name: "both within constraints",
			input: LengthStruct{
				ShortField: "abc",
				LongField:  "this is a long field",
			},
			valid: true,
		},
		{
			name: "short field below min",
			input: LengthStruct{
				ShortField: "",
				LongField:  "this is a long field",
			},
			valid: false,
		},
		{
			name: "short field above max",
			input: LengthStruct{
				ShortField: "abcdef",
				LongField:  "this is a long field",
			},
			valid: false,
		},
		{
			name: "long field below min",
			input: LengthStruct{
				ShortField: "abc",
				LongField:  "short",
			},
			valid: false,
		},
		{
			name: "long field above max",
			input: LengthStruct{
				ShortField: "abc",
				LongField:  string(make([]byte, 101)),
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(tt.input)
			if tt.valid && err != nil {
				t.Errorf("expected valid struct, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected validation error, but validation passed")
			}
		})
	}
}

// BenchmarkValidateStruct benchmarks struct validation
func BenchmarkValidateStruct(b *testing.B) {
	type TestStruct struct {
		Name  string `json:"name" validate:"required,min=3,max=32"`
		Email string `json:"email" validate:"required,email"`
		Age   int    `json:"age" validate:"required,min=0,max=150"`
	}

	validator := New()
	input := TestStruct{
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   25,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateStruct(input)
	}
}

// BenchmarkValidateVar benchmarks variable validation
func BenchmarkValidateVar(b *testing.B) {
	validator := New()
	email := "test@example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateVar(email, "email")
	}
}
