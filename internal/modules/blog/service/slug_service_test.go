package service

import "testing"

func TestSlugService_Generate(t *testing.T) {
	svc := NewSlugService()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "normal English text",
			input: "Hello World",
			want:  "hello-world",
		},
		{
			name:  "lowercase English with punctuation",
			input: "Go is awesome!",
			want:  "go-is-awesome",
		},
		{
			name:  "Turkish ş to s",
			input: "şeker",
			want:  "seker",
		},
		{
			name:  "Turkish ç to c",
			input: "çiçek",
			want:  "cicek",
		},
		{
			name:  "Turkish ğ to g",
			input: "dağ",
			want:  "dag",
		},
		{
			name:  "Turkish ı to i",
			input: "ışık",
			want:  "isik",
		},
		{
			name:  "Turkish ö to o",
			input: "göz",
			want:  "goz",
		},
		{
			name:  "Turkish ü to u",
			input: "üzüm",
			want:  "uzum",
		},
		{
			name:  "Turkish uppercase characters",
			input: "Şehir Çarşısı",
			want:  "sehir-carsisi",
		},
		{
			name:  "full Turkish sentence",
			input: "Türkçe Karakterler Ğüşıöç",
			want:  "turkce-karakterler-gusioc",
		},
		{
			name:  "special characters removed",
			input: "hello@world!.com#test",
			want:  "helloworldcomtest",
		},
		{
			name:  "ampersand and symbols",
			input: "cats & dogs (fun)",
			want:  "cats-dogs-fun",
		},
		{
			name:  "multiple spaces collapsed to single dash",
			input: "hello   world",
			want:  "hello-world",
		},
		{
			name:  "multiple dashes collapsed",
			input: "hello---world",
			want:  "hello-world",
		},
		{
			name:  "leading and trailing dashes trimmed",
			input: "---hello world---",
			want:  "hello-world",
		},
		{
			name:  "leading dash from special char trimmed",
			input: "!hello",
			want:  "hello",
		},
		{
			name:  "underscores replaced with dashes",
			input: "hello_world",
			want:  "hello-world",
		},
		{
			name:  "numbers preserved",
			input: "Top 10 Go Tips",
			want:  "top-10-go-tips",
		},
		{
			name:  "already valid slug unchanged",
			input: "clean-slug",
			want:  "clean-slug",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.Generate(tt.input)
			if got != tt.want {
				t.Errorf("Generate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
