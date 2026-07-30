// SPDX-License-Identifier: MIT

package genir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmit_MkdirFailure(t *testing.T) {
	block := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEmitter(&Resolved{Schema: &Schema{Bundle: "b", Version: "v", Namespace: "ns"}}, block, "pkg")
	if err := e.Emit(); err == nil {
		t.Fatal("expected MkdirAll failure")
	}
}

func TestEmit_WriteFileFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "doc.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := NewEmitter(&Resolved{Schema: &Schema{Bundle: "b", Version: "v", Namespace: "ns"}}, dir, "pkg")
	if err := e.Emit(); err == nil {
		t.Fatal("expected emitDoc write failure")
	}
}

func TestResolveElementType_Branches(t *testing.T) {
	r := &Resolved{
		Schema: &Schema{},
		ComplexTypeMap: map[string]*ComplexType{
			"tBase": {Name: "tBase"},
		},
		SimpleTypeMap: map[string]*SimpleType{
			"tEnum":  {Name: "tEnum", Enumerations: []string{"a"}},
			"tAlias": {Name: "tAlias", Base: "xs:int"},
			"tOther": {Name: "tOther", Base: "tMystery"},
		},
		ElementMap: map[string]*Element{
			"RefEl": {
				Name: "RefEl",
				InlineComplex: &ComplexType{
					ContentKind: ContentComplexExtend,
					BaseType:    "tBase",
				},
			},
			"RefInline": {
				Name: "RefInline",
				InlineComplex: &ComplexType{
					ContentKind: ContentComplexExtend,
					BaseType:    "missing",
				},
			},
		},
	}
	e := NewEmitter(r, t.TempDir(), "pkg")

	if got := e.resolveElementType(&Element{
		Name: "E",
		InlineComplex: &ComplexType{
			ContentKind: ContentComplexExtend,
			BaseType:    "tBase",
		},
	}); got != "Base" {
		t.Errorf("empty extension base = %q, want Base", got)
	}
	if got := e.resolveElementType(&Element{
		Name: "E2",
		InlineComplex: &ComplexType{
			ContentKind: ContentDirect,
		},
	}); got != "E2Inline" {
		t.Errorf("inline = %q", got)
	}
	if got := e.resolveElementType(&Element{Ref: "RefEl"}); got != "Base" {
		t.Errorf("ref empty-ext = %q", got)
	}
	if got := e.resolveElementType(&Element{Ref: "RefInline"}); got != "RefInlineInline" {
		t.Errorf("ref inline = %q", got)
	}
	if got := e.resolveElementType(&Element{}); got != "string" {
		t.Errorf("empty = %q", got)
	}
	if got := e.resolveElementType(&Element{Type: "xs:boolean"}); got != "bool" {
		t.Errorf("builtin = %q", got)
	}
	if got := e.resolveElementType(&Element{Type: "tBase"}); got != "Base" {
		t.Errorf("complex = %q", got)
	}
	if got := e.resolveElementType(&Element{Type: "tEnum"}); got != "Enum" {
		t.Errorf("enum = %q", got)
	}
	if got := e.resolveElementType(&Element{Type: "tOther"}); got != "string" {
		t.Errorf("other simple = %q", got)
	}
	if got := e.resolveElementType(&Element{Type: "unknown"}); got != "string" {
		t.Errorf("unknown = %q", got)
	}
}

func TestResolveAttrType_Branches(t *testing.T) {
	r := &Resolved{
		Schema: &Schema{},
		SimpleTypeMap: map[string]*SimpleType{
			"tEnum":  {Name: "tEnum", Enumerations: []string{"a"}},
			"tAlias": {Name: "tAlias", Base: "xs:string"},
			"tOther": {Name: "tOther", Base: "custom"},
		},
	}
	e := NewEmitter(r, t.TempDir(), "pkg")

	if got := e.resolveAttrType(&Attribute{InlineSimple: &SimpleType{Enumerations: []string{"x"}}}); got != "string" {
		t.Errorf("inline enum = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{InlineSimple: &SimpleType{Base: "xs:int"}}); got != "int" {
		t.Errorf("inline base = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{InlineSimple: &SimpleType{}}); got != "string" {
		t.Errorf("inline empty = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{}); got != "string" {
		t.Errorf("empty = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{Type: "xs:boolean"}); got != "bool" {
		t.Errorf("builtin = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{Type: "tEnum"}); got != "Enum" {
		t.Errorf("enum = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{Type: "tAlias"}); got != "string" {
		t.Errorf("alias = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{Type: "tOther"}); got != "string" {
		t.Errorf("other = %q", got)
	}
	if got := e.resolveAttrType(&Attribute{Type: "missing"}); got != "string" {
		t.Errorf("missing = %q", got)
	}
}

func TestEmit_EnumsTypesSCLRoot(t *testing.T) {
	base := &ComplexType{Name: "tBase", Sequence: []*Element{{Name: "Child", Type: "xs:string"}}}
	simple := &ComplexType{
		Name:        "tSimple",
		ContentKind: ContentSimpleExtend,
		BaseType:    "xs:string",
		Attributes:  []*Attribute{{Name: "lang", Type: "xs:string"}, {Name: ""}},
	}
	choice := &ChoiceGroup{Elements: []*Element{
		{Name: "A", Type: "xs:string"},
		{Ref: "Header"},
		{Name: "A"},
	}}
	sclInline := &ComplexType{
		ContentKind:   ContentDirect,
		HasAnyElement: true,
		Sequence: []*Element{
			{Name: "Header", Type: "xs:string"},
			{Ref: "Header"},
			{Name: "IED", Type: "xs:string"},
		},
		Choice: choice,
		Attributes: []*Attribute{
			{Name: "version", Type: "xs:string"},
			{Name: "version"},
			{Name: ""},
		},
	}
	refInlineEl := &Element{
		Name: "Opt",
		InlineComplex: &ComplexType{
			ContentKind: ContentDirect,
			Sequence:    []*Element{{Name: "X", Type: "xs:int"}},
		},
	}
	emptyExtEl := &Element{
		Name: "Alias",
		InlineComplex: &ComplexType{
			ContentKind: ContentComplexExtend,
			BaseType:    "tBase",
		},
	}

	r := &Resolved{
		Schema: &Schema{
			Bundle:    "b",
			Version:   "v",
			Namespace: "http://example",
			SimpleTypes: []*SimpleType{
				{Name: "tEnum", Enumerations: []string{"a-b", "a_b", "@@@"}},
			},
			ComplexTypes: []*ComplexType{
				{Name: ""},
				base,
				simple,
				{Name: "tAbs", Abstract: true},
				{
					Name: "tWithInline",
					Sequence: []*Element{
						{Ref: "Opt"},
						{Ref: "Alias"},
						{Name: "Own", InlineComplex: &ComplexType{Sequence: []*Element{{Name: "Y", Type: "xs:boolean"}}}},
					},
					Choice: choice,
				},
			},
		},
		ComplexTypeMap: map[string]*ComplexType{"tBase": base, "tSimple": simple},
		SimpleTypeMap:  map[string]*SimpleType{"tEnum": {Name: "tEnum", Enumerations: []string{"a-b", "a_b", "@@@"}}},
		ElementMap: map[string]*Element{
			"Opt":    refInlineEl,
			"Alias":  emptyExtEl,
			"Header": {Name: "Header", Type: "xs:string"},
		},
		TopElements: []*Element{{
			Name:          "SCL",
			InlineComplex: sclInline,
		}},
	}
	dir := t.TempDir()
	e := NewEmitter(r, dir, "pkg")
	if err := e.Emit(); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "enums.go")); err != nil {
		t.Fatal("enums.go missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "types.go")); err != nil {
		t.Fatal("types.go missing")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "types.go"))
	if !strings.Contains(string(data), "type SCL struct") {
		t.Fatal("SCL root missing")
	}
	if !strings.Contains(string(data), "ExtraElements") {
		t.Fatal("HasAnyElement field missing")
	}
}

func TestHasAnyElementAndAttr(t *testing.T) {
	baseAnyEl := &ComplexType{Name: "BaseEl", HasAnyElement: true}
	baseAnyAttr := &ComplexType{Name: "BaseAttr", HasAnyAttr: true}
	child := &ComplexType{Name: "Child", BaseType: "BaseEl"}
	childAttr := &ComplexType{Name: "ChildAttr", BaseType: "BaseAttr"}
	r := &Resolved{
		Schema: &Schema{},
		ComplexTypeMap: map[string]*ComplexType{
			"BaseEl":    baseAnyEl,
			"BaseAttr":  baseAnyAttr,
			"Child":     child,
			"ChildAttr": childAttr,
		},
	}
	e := NewEmitter(r, t.TempDir(), "pkg")

	if !e.hasAnyElement(&ComplexType{HasAnyElement: true}) {
		t.Fatal("direct HasAnyElement")
	}
	if !e.hasAnyElement(child) {
		t.Fatal("inherited HasAnyElement")
	}
	if e.hasAnyElement(&ComplexType{Name: "plain"}) {
		t.Fatal("plain should be false")
	}
	if !e.hasAnyAttr(&ComplexType{HasAnyAttr: true}) {
		t.Fatal("direct HasAnyAttr")
	}
	if !e.hasAnyAttr(childAttr) {
		t.Fatal("inherited HasAnyAttr")
	}
	if e.hasAnyAttr(&ComplexType{Name: "plain"}) {
		t.Fatal("plain attr should be false")
	}
}
