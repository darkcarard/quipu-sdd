package feature

import (
	"errors"
	"strings"
	"testing"

	"github.com/darkcarard/quipu-sdd/internal/config"
	"github.com/darkcarard/quipu-sdd/internal/journal"
	"github.com/darkcarard/quipu-sdd/internal/workflow"
)

func TestValidName(t *testing.T) {
	tests := []struct {
		name string
		want string // substring of the error; "" means valid
	}{
		{"user-signup", ""},
		{"a1", ""},
		{"v2-api", ""},
		{strings.Repeat("a", MaxNameLen), ""},
		{"", "empty"},
		{strings.Repeat("a", MaxNameLen+1), "the limit is 40"},
		{"User", "invalid"},
		{"user_signup", "invalid"},
		{"user signup", "invalid"},
		{"-x", "invalid"},
		{"x-", "invalid"},
		{"a--b", "invalid"},
		{"señal", "invalid"},
		{"../etc", "invalid"},
		{"con", "reserved"},
		{"lpt1", "reserved"},
		{"new", "reserved"},
		{"approve", "reserved"},
	}
	for _, tt := range tests {
		err := ValidName(tt.name)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("ValidName(%q) = %v, want valid", tt.name, err)
		case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
			t.Errorf("ValidName(%q) = %v, want error containing %q", tt.name, err, tt.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want, err string
	}{
		{"User Signup", "user-signup", ""},
		{"Registro de Usuarios (v2)!", "registro-de-usuarios-v2", ""},
		{"Facturación electrónica: Año 2026", "facturacion-electronica-ano-2026", ""},
		{"  multiple   spaces__and--dashes ", "multiple-spaces-and-dashes", ""},
		{"Straße Œuvre Ærø", "strasse-oeuvre-aero", ""},
		{"api 日本語 v2", "api-v2", ""},
		{"Integración con el sistema de facturación electrónica de la DIAN", "integracion-con-el-sistema", ""},
		{"Synchronization of the customer accounts with the ERP", "synchronization-of-the-customer-accounts", ""},
		{"Gestión de cuentas por cobrar de los clientes", "gestion-de-cuentas-por-cobrar", ""}, // "-de-los" dropped
		{"De la A a la Z", "de-la-a-a-la-z", ""},                                               // not cut: stopwords kept
		{"Plan A", "plan-a", ""},
		{strings.Repeat("x", 50), strings.Repeat("x", MaxNameLen), ""},
		{"🚀 !!", "", "cannot derive"},
		{"NUL", "", "reserved"},
		{"Help", "", "derived from \"Help\""},
	}
	for _, tt := range tests {
		got, err := Slugify(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("Slugify(%q) = %q, %v; want error containing %q", tt.in, got, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("Slugify(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
		if len(got) > MaxNameLen {
			t.Errorf("Slugify(%q) is longer than %d", tt.in, MaxNameLen)
		}
	}
}

func TestResolve(t *testing.T) {
	l := setup(t)
	f, err := Open(l, config.Default(), "registro-de-usuarios")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Create(workflow.Functional, Info{Title: "Alta de clientes", Description: "d"}, who, now); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"registro-de-usuarios", "Registro de Usuarios", "alta de CLIENTES"} {
		if got, err := Resolve(l, arg); err != nil || got != "registro-de-usuarios" {
			t.Errorf("Resolve(%q) = %q, %v", arg, got, err)
		}
	}
	for _, arg := range []string{"otra", "Otra cosa", "🚀"} {
		if _, err := Resolve(l, arg); !errors.Is(err, ErrNotFound) {
			t.Errorf("Resolve(%q) = %v, want ErrNotFound", arg, err)
		}
	}
	st, err := l.Load("registro-de-usuarios")
	if err != nil || st.Title != "Alta de clientes" || st.Description != "d" || st.CreatedBy != who.String() || !st.CreatedAt.Equal(now) {
		t.Errorf("state = %+v, %v", st, err)
	}
}

func TestDescribe(t *testing.T) {
	l := setup(t)
	f, _ := Open(l, config.Default(), "pagos")
	if _, err := f.Create(workflow.Functional, Info{Title: "Pagos"}, who, now); err != nil {
		t.Fatal(err)
	}
	str := func(s string) *string { return &s }

	if changed, err := f.Describe(str("  Pagos   con tarjeta "), str("Cobro"), who); err != nil || !changed {
		t.Fatalf("Describe = %v, %v", changed, err)
	}
	if changed, err := f.Describe(nil, str(""), who); err != nil || !changed {
		t.Fatalf("clearing the description = %v, %v", changed, err)
	}
	if changed, _ := f.Describe(str("Pagos con tarjeta"), nil, who); changed {
		t.Error("an unchanged title must not be recorded")
	}
	if _, err := f.Describe(str(" "), nil, who); err == nil {
		t.Error("an empty title must be rejected")
	}

	st, _ := l.Load("pagos")
	if st.Title != "Pagos con tarjeta" || st.Description != "" {
		t.Errorf("state = %+v", st)
	}
	if got, err := Resolve(l, "pagos con TARJETA"); err != nil || got != "pagos" {
		t.Errorf("Resolve by the new title = %q, %v", got, err)
	}
	events, _ := journal.Read(l.JournalFile("pagos"))
	var details []string
	for _, ev := range events {
		if ev.Action == "feature.describe" {
			details = append(details, ev.Detail)
		}
	}
	if len(details) != 2 || !strings.Contains(details[0], `title "Pagos" → "Pagos con tarjeta"`) {
		t.Errorf("journal = %q", details)
	}
}
