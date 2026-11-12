package rules

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
)

type sharedCase struct {
	Name    string `json:"name"`
	Request struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"request"`
	Matcher *struct {
		Host      string `json:"host"`
		PathRegex string `json:"path_regex"`
		Method    string `json:"method"`
	} `json:"matcher"`
	Match *bool `json:"match"`
	Field *struct {
		In      string `json:"in"`
		Path    string `json:"path"`
		RegexOn string `json:"regex_on"`
	} `json:"field"`
	OK    *bool  `json:"ok"`
	Value string `json:"value"`
}

func loadSharedCases(t *testing.T) []sharedCase {
	t.Helper()
	data, err := os.ReadFile("testdata/extract_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []sharedCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("没有样例")
	}
	return file.Cases
}

func TestSharedCases(t *testing.T) {
	for _, c := range loadSharedCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range c.Request.Headers {
				h.Add(k, v)
			}
			req := Request{Method: c.Request.Method, URL: c.Request.URL, Headers: h, Body: []byte(c.Request.Body)}
			if c.Matcher != nil {
				m := Matcher{Host: c.Matcher.Host, PathRegex: c.Matcher.PathRegex, Method: c.Matcher.Method}
				if err := m.Validate(); err != nil {
					t.Fatalf("样例里的匹配规则不合法：%v", err)
				}
				if got := m.Match(req); got != *c.Match {
					t.Errorf("Match = %v，期望 %v", got, *c.Match)
				}
			}
			if c.Field != nil {
				f := Field{In: c.Field.In, Path: c.Field.Path, RegexOn: c.Field.RegexOn}
				if err := f.Validate(); err != nil {
					t.Fatalf("样例里的取值规则不合法：%v", err)
				}
				v, ok, err := f.Extract(req)
				if err != nil {
					t.Fatal(err)
				}
				if ok != *c.OK || (ok && v != c.Value) {
					t.Errorf("Extract = %q, %v；期望 %q, %v", v, ok, c.Value, *c.OK)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	badMatchers := []Matcher{
		{Host: ""}, {Host: "*."}, {Host: "a.*.com"}, {Host: "https://a.com"}, {Host: "a.com:443"},
		{Host: ".a.com"}, {Host: "a.com", PathRegex: "("}, {Host: "a.com", PathRegex: "^/a{1,3}"}, {Host: "a.com", PathRegex: "^/a b"},
	}
	for _, m := range badMatchers {
		if m.Validate() == nil {
			t.Errorf("匹配规则 %+v 应当不合法", m)
		}
	}
	badFields := []Field{
		{In: "nowhere", Path: "x"}, {In: InHeader}, {In: InRegex, Path: "abc", RegexOn: RegexOnURL},
		{In: InRegex, Path: "(a", RegexOn: RegexOnURL}, {In: InRegex, Path: "(a)"},
	}
	for _, f := range badFields {
		if f.Validate() == nil {
			t.Errorf("取值规则 %+v 应当不合法", f)
		}
	}
}

// 校验错误指出出错的是哪一项。
func TestValidatePart(t *testing.T) {
	part := func(err error) string {
		var pe *PartError
		if errors.As(err, &pe) {
			return pe.Part
		}
		return ""
	}
	cases := []struct {
		err  error
		want string
	}{
		{Matcher{Host: "https://a.com"}.Validate(), "host"},
		{Matcher{Host: "a.com", PathRegex: "("}.Validate(), "path"},
		{Matcher{Host: "a.com", PathRegex: "^/a b"}.Validate(), "path"},
		{Field{In: "nowhere", Path: "x"}.Validate(), "in"},
		{Field{In: InHeader}.Validate(), "path"},
		{Field{In: InRegex, Path: "abc", RegexOn: RegexOnURL}.Validate(), "path"},
		{Field{In: InRegex, Path: "(a)"}.Validate(), "regex_on"},
	}
	for i, c := range cases {
		if got := part(c.err); got != c.want {
			t.Errorf("用例 %d：part = %q，期望 %q（%v）", i, got, c.want, c.err)
		}
	}
}

func TestNeedsBody(t *testing.T) {
	cases := map[Field]bool{
		{In: InHeader, Path: "a"}:                        false,
		{In: InBodyJSON, Path: "a"}:                      true,
		{In: InBodyForm, Path: "a"}:                      true,
		{In: InRegex, Path: "(a)", RegexOn: RegexOnURL}:  false,
		{In: InRegex, Path: "(a)", RegexOn: RegexOnBody}: true,
	}
	for f, want := range cases {
		if f.NeedsBody() != want {
			t.Errorf("%+v NeedsBody = %v", f, !want)
		}
	}
}
