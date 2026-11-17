// Package diag defines the diagnostics produced by spec lint, plan validation
// and sync checks.
package diag

import (
	"fmt"
	"sort"
)

// Severity of a diagnostic. Errors block approvals; warnings never do.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Diagnostic is one finding, located at File:Line when known (Line 0 means the whole file).
type Diagnostic struct {
	Severity Severity
	File     string
	Line     int
	Message  string
}

func (d Diagnostic) String() string {
	loc := d.File
	if d.Line > 0 {
		loc = fmt.Sprintf("%s:%d", d.File, d.Line)
	}
	if loc == "" {
		return fmt.Sprintf("%s: %s", d.Severity, d.Message)
	}
	return fmt.Sprintf("%s: %s: %s", loc, d.Severity, d.Message)
}

// List is a collection of diagnostics.
type List []Diagnostic

// Errorf appends an error at line.
func (l *List) Errorf(line int, format string, args ...any) {
	*l = append(*l, Diagnostic{Severity: Error, Line: line, Message: fmt.Sprintf(format, args...)})
}

// Warnf appends a warning at line.
func (l *List) Warnf(line int, format string, args ...any) {
	*l = append(*l, Diagnostic{Severity: Warning, Line: line, Message: fmt.Sprintf(format, args...)})
}

// Count returns the number of errors and warnings.
func (l List) Count() (errors, warnings int) {
	for _, d := range l {
		if d.Severity == Error {
			errors++
		} else {
			warnings++
		}
	}
	return errors, warnings
}

// HasErrors reports whether any diagnostic is an error.
func (l List) HasErrors() bool {
	e, _ := l.Count()
	return e > 0
}

// InFile returns a copy of l with File set on every diagnostic that has none.
func (l List) InFile(file string) List {
	out := make(List, len(l))
	for i, d := range l {
		if d.File == "" {
			d.File = file
		}
		out[i] = d
	}
	return out
}

// Sorted returns a copy of l ordered by file, line, then severity (errors first).
func (l List) Sorted() List {
	out := append(List(nil), l...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Severity == Error && b.Severity != Error
	})
	return out
}
