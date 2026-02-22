package admin

import (
	"bytes"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name         string
		templateName string
		data         any
		wantContent  string
	}{
		{
			name:         "login template",
			templateName: "login.html",
			data:         map[string]any{"Error": "invalid credentials"},
			wantContent:  "Login",
		},
		{
			name:         "setup template",
			templateName: "setup.html",
			data:         nil,
			wantContent:  "Setup Admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := Render(&buf, tt.templateName, tt.data)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if !strings.Contains(buf.String(), tt.wantContent) {
				t.Errorf("Render() content missing %q", tt.wantContent)
			}
		})
	}
}
