package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// operation 为需要计算哈希的提交操作。
const operation = "CreatePublishedFormEntry"

var httpClient = &http.Client{Timeout: 30 * time.Second}

func fetch(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func fetchAll(urls []string) []string {
	res := make([]string, len(urls))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if t, err := fetch(u); err == nil {
				res[i] = t
			}
		}(i, u)
	}
	wg.Wait()
	return res
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------- JS 字符串 ----------------
func readJSString(s string, i int) (string, int) {
	quote := s[i]
	i++
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s) {
				break
			}
			switch n := s[i+1]; n {
			case 'n':
				b.WriteByte('\n')
				i += 2
				continue
			case 't':
				b.WriteByte('\t')
				i += 2
				continue
			case 'r':
				b.WriteByte('\r')
				i += 2
				continue
			case 'b':
				b.WriteByte('\b')
				i += 2
				continue
			case 'f':
				b.WriteByte('\f')
				i += 2
				continue
			case 'v':
				b.WriteByte('\v')
				i += 2
				continue
			case 'u':
				if i+6 <= len(s) {
					if v, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 6
						continue
					}
				}
				b.WriteByte(n)
				i += 2
				continue
			case 'x':
				if i+4 <= len(s) {
					if v, err := strconv.ParseUint(s[i+2:i+4], 16, 32); err == nil {
						b.WriteByte(byte(v))
						i += 4
						continue
					}
				}
				b.WriteByte(n)
				i += 2
				continue
			default:
				b.WriteByte(n)
				i += 2
				continue
			}
		}
		if c == quote {
			return b.String(), i + 1
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i
}

// ---------------- 新版前端: t.exports="..." / ["...",e.r(id)].join("") ----------------
type nfPart struct{ lit, ref string }
type nfMod struct {
	str   *string
	parts []nfPart
	sep   string
}

var (
	nfModRe   = regexp.MustCompile(`(\d+),\((?:\w+,)*\w+\)=>\{t\.exports=(\[|")`)
	nfRefRe   = regexp.MustCompile(`^e\.r\((\d+)\)`)
	nfSepRe   = regexp.MustCompile(`^\.join\("((?:[^"\\]|\\.)*)"\)`)
	jsSrcRe   = regexp.MustCompile(`src="([^"]+\.js[^"]*)"`)
	identChar = func(c byte) bool {
		return c == '_' || c == '$' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
)

func collectNextfe(texts []string) map[string]*nfMod {
	mods := map[string]*nfMod{}
	for _, text := range texts {
		idx := nfModRe.FindAllStringSubmatchIndex(text, -1)
		for _, m := range idx {
			id := text[m[2]:m[3]]
			open := m[4]
			if text[open] == '"' {
				v, _ := readJSString(text, open)
				mods[id] = &nfMod{str: &v}
				continue
			}
			j := open + 1
			var parts []nfPart
			for j < len(text) && text[j] != ']' {
				if text[j] == '"' {
					v, nj := readJSString(text, j)
					parts = append(parts, nfPart{lit: v})
					j = nj
					continue
				}
				if rm := nfRefRe.FindStringSubmatch(text[j:]); rm != nil {
					parts = append(parts, nfPart{ref: rm[1]})
					j += len(rm[0])
					continue
				}
				j++
			}
			sep := ""
			if j < len(text) {
				tail := text[j+1:]
				if len(tail) > 24 {
					tail = tail[:24]
				}
				if sm := nfSepRe.FindStringSubmatch(tail); sm != nil {
					sep = sm[1]
				}
			}
			mods[id] = &nfMod{parts: parts, sep: sep}
		}
	}
	return mods
}

func resolveNextfe(mods map[string]*nfMod, id string, seen map[string]bool) string {
	if seen[id] {
		return ""
	}
	seen[id] = true
	m := mods[id]
	if m == nil {
		return ""
	}
	if m.str != nil {
		return *m.str
	}
	var b strings.Builder
	for _, p := range m.parts {
		if p.ref != "" {
			b.WriteString(resolveNextfe(mods, p.ref, seen))
		} else {
			b.WriteString(p.lit)
		}
	}
	return b.String()
}

// ---------------- 旧版前端: 编译后的 AST 字面量 + .concat(i(id)) ----------------
type legacyDoc struct {
	ast  any
	refs []string
}

var (
	legacyLitRe   = regexp.MustCompile(`\{kind:"Document"`)
	moduleHeadRe  = regexp.MustCompile(`[,{;](\d{1,9}):function\([\w,]+\)\{`)
	concatRefRe   = regexp.MustCompile(`\.concat\(i\((\d+)\)\.definitions`)
	numberLiteral = regexp.MustCompile(`^-?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?`)
)

func balancedEnd(s string, start int) int {
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func extractLegacyDocs(text string) map[string]*legacyDoc {
	headers := moduleHeadRe.FindAllStringSubmatchIndex(text, -1)
	docs := map[string]*legacyDoc{}
	for _, lm := range legacyLitRe.FindAllStringIndex(text, -1) {
		start := lm[0]
		end := balancedEnd(text, start)
		if end < 0 {
			continue
		}
		id := ""
		for _, h := range headers {
			if h[0] < start {
				id = text[h[2]:h[3]]
			} else {
				break
			}
		}
		if id == "" {
			continue
		}
		limit := end + 30000
		if limit > len(text) {
			limit = len(text)
		}
		for _, h := range headers {
			if h[0] > end && h[0] < limit {
				limit = h[0]
				break
			}
		}
		var refs []string
		for _, cm := range concatRefRe.FindAllStringSubmatch(text[end:limit], -1) {
			refs = append(refs, cm[1])
		}
		if v, err := parseJSValue(text, start); err == nil {
			docs[id] = &legacyDoc{ast: v, refs: refs}
		}
	}
	return docs
}

// ---------------- JS 对象字面量解析 ----------------
type jsParser struct {
	s string
	i int
}

func parseJSValue(s string, i int) (any, error) {
	p := &jsParser{s: s, i: i}
	return p.value()
}

func (p *jsParser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsParser) value() (any, error) {
	p.ws()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected eof")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"' || c == '\'':
		v, ni := readJSString(p.s, p.i)
		p.i = ni
		return v, nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return p.ident()
	}
}

func (p *jsParser) object() (any, error) {
	obj := map[string]any{}
	p.i++ // '{'
	for {
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("eof in object")
		}
		if p.s[p.i] == '}' {
			p.i++
			return obj, nil
		}
		var key string
		if p.s[p.i] == '"' || p.s[p.i] == '\'' {
			key, p.i = readJSString(p.s, p.i)
		} else {
			begin := p.i
			for p.i < len(p.s) && identChar(p.s[p.i]) {
				p.i++
			}
			key = p.s[begin:p.i]
		}
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("expected ':'")
		}
		p.i++
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		obj[key] = v
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return obj, nil
		}
		return nil, fmt.Errorf("expected ',' or '}'")
	}
}

func (p *jsParser) array() (any, error) {
	arr := []any{}
	p.i++ // '['
	for {
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("eof in array")
		}
		if p.s[p.i] == ']' {
			p.i++
			return arr, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return arr, nil
		}
		return nil, fmt.Errorf("expected ',' or ']'")
	}
}

func (p *jsParser) number() (any, error) {
	m := numberLiteral.FindString(p.s[p.i:])
	if m == "" {
		return nil, fmt.Errorf("bad number")
	}
	p.i += len(m)
	f, err := strconv.ParseFloat(m, 64)
	return f, err
}

func (p *jsParser) ident() (any, error) {
	begin := p.i
	for p.i < len(p.s) && identChar(p.s[p.i]) {
		p.i++
	}
	switch word := p.s[begin:p.i]; word {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "undefined":
		return nil, nil
	default:
		return word, nil
	}
}

// ---------------- 旧版 graphql 打印器 (graphql-js v14 规则) ----------------
func anySlice(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func kindOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		if k, ok := m["kind"].(string); ok {
			return k
		}
	}
	return ""
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

func nameValue(v any) string {
	if m, ok := v.(map[string]any); ok {
		return strOf(m["value"])
	}
	return ""
}

func valueText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return ""
}

func joinStrings(parts []string, sep string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func printAll(v any) []string {
	var out []string
	for _, e := range anySlice(v) {
		out = append(out, printAst(e))
	}
	return out
}

func wrap(start, mid string, end ...string) string {
	if mid == "" {
		return ""
	}
	if len(end) > 0 {
		return start + mid + end[0]
	}
	return start + mid
}

func indentStr(s string) string {
	return wrap("  ", strings.ReplaceAll(s, "\n", "\n  "), "")
}

func block(items []string) string {
	return wrap("{\n", indentStr(joinStrings(items, "\n")), "\n}")
}

func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func printAst(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	switch strOf(m["kind"]) {
	case "Document":
		return joinStrings(printAll(m["definitions"]), "\n\n") + "\n"
	case "OperationDefinition":
		varDefs := wrap("(", joinStrings(printAll(m["variableDefinitions"]), ", "), ")")
		dirs := joinStrings(printAll(m["directives"]), " ")
		name := nameValue(m["name"])
		if name == "" && dirs == "" && varDefs == "" && strOf(m["operation"]) == "query" {
			return printAst(m["selectionSet"])
		}
		return joinStrings([]string{strOf(m["operation"]), joinStrings([]string{name, varDefs}, ""), dirs, printAst(m["selectionSet"])}, " ")
	case "VariableDefinition":
		return printAst(m["variable"]) + ": " + printAst(m["type"]) +
			wrap(" = ", printAst(m["defaultValue"])) + wrap(" ", joinStrings(printAll(m["directives"]), " "))
	case "SelectionSet":
		return block(printAll(m["selections"]))
	case "Field":
		prefix := wrap("", printAst(m["alias"]), ": ") + nameValue(m["name"])
		return joinStrings([]string{
			prefix + wrap("(", joinStrings(printAll(m["arguments"]), ", "), ")"),
			joinStrings(printAll(m["directives"]), " "),
			printAst(m["selectionSet"]),
		}, " ")
	case "Argument":
		return nameValue(m["name"]) + ": " + printAst(m["value"])
	case "FragmentSpread":
		return "..." + nameValue(m["name"]) + wrap(" ", joinStrings(printAll(m["directives"]), " "))
	case "InlineFragment":
		return joinStrings([]string{"...", wrap("on ", printAst(m["typeCondition"])),
			joinStrings(printAll(m["directives"]), " "), printAst(m["selectionSet"])}, " ")
	case "FragmentDefinition":
		varDefs := wrap("(", joinStrings(printAll(m["variableDefinitions"]), ", "), ")")
		return "fragment " + nameValue(m["name"]) + varDefs + " on " + printAst(m["typeCondition"]) + " " +
			wrap("", joinStrings(printAll(m["directives"]), " "), " ") + printAst(m["selectionSet"])
	case "Name":
		return strOf(m["value"])
	case "Variable":
		return "$" + nameValue(m["name"])
	case "IntValue", "FloatValue", "EnumValue":
		return valueText(m["value"])
	case "StringValue":
		return jsonString(strOf(m["value"]))
	case "BooleanValue":
		if b, _ := m["value"].(bool); b {
			return "true"
		}
		return "false"
	case "NullValue":
		return "null"
	case "ListValue":
		return "[" + joinStrings(printAll(m["values"]), ", ") + "]"
	case "ObjectValue":
		return "{" + joinStrings(printAll(m["fields"]), ", ") + "}"
	case "ObjectField":
		return nameValue(m["name"]) + ": " + printAst(m["value"])
	case "Directive":
		return "@" + nameValue(m["name"]) + wrap("(", joinStrings(printAll(m["arguments"]), ", "), ")")
	case "NamedType":
		return nameValue(m["name"])
	case "ListType":
		return "[" + printAst(m["type"]) + "]"
	case "NonNullType":
		return printAst(m["type"]) + "!"
	}
	return ""
}

// ---------------- Apollo addTypename ----------------
func addTypename(v any, parentKind string) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = addTypename(e, parentKind)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x)+1)
		for k, val := range x {
			if k == "loc" {
				continue
			}
			out[k] = addTypename(val, kindOf(x))
		}
		if kindOf(x) == "SelectionSet" && parentKind != "OperationDefinition" {
			has := false
			for _, s := range anySlice(x["selections"]) {
				if kindOf(s) == "Field" && nameValue(fieldNameOf(s)) == "__typename" {
					has = true
					break
				}
			}
			if !has {
				sel := anySlice(out["selections"])
				out["selections"] = append(sel, map[string]any{
					"kind": "Field",
					"name": map[string]any{"kind": "Name", "value": "__typename"},
				})
			}
		}
		return out
	default:
		return v
	}
}

func fieldNameOf(v any) any {
	if m, ok := v.(map[string]any); ok {
		return m["name"]
	}
	return nil
}

// ---------------- 旧版文档合并 ----------------
func legacyQuery(docs map[string]*legacyDoc, op string) string {
	for id, d := range docs {
		dm, ok := d.ast.(map[string]any)
		if !ok {
			continue
		}
		found := false
		for _, def := range anySlice(dm["definitions"]) {
			if kindOf(def) == "OperationDefinition" && nameValue(fieldNameOf(def)) == op {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		defs := append([]any{}, anySlice(dm["definitions"])...)
		seen := map[string]bool{id: true}
		var add func(string)
		add = func(rid string) {
			if seen[rid] {
				return
			}
			seen[rid] = true
			rd := docs[rid]
			if rd == nil {
				return
			}
			rm, _ := rd.ast.(map[string]any)
			for _, def := range anySlice(rm["definitions"]) {
				if kindOf(def) != "FragmentDefinition" {
					continue
				}
				name := nameValue(fieldNameOf(def))
				dup := false
				for _, e := range defs {
					if kindOf(e) == "FragmentDefinition" && nameValue(fieldNameOf(e)) == name {
						dup = true
						break
					}
				}
				if !dup {
					defs = append(defs, def)
				}
			}
			for _, r := range rd.refs {
				add(r)
			}
		}
		for _, r := range d.refs {
			add(r)
		}
		return printAst(addTypename(map[string]any{"kind": "Document", "definitions": defs}, ""))
	}
	return ""
}

func sign(pageURL string) (string, error) {
	html, err := fetch(pageURL)
	if err != nil {
		return "", err
	}

	base, _ := url.Parse(pageURL)
	seen := map[string]bool{}
	var jsURLs []string
	for _, m := range jsSrcRe.FindAllStringSubmatch(html, -1) {
		ref, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		u := base.ResolveReference(ref).String()
		if !seen[u] && (strings.Contains(u, "/gd-frontend/") || strings.Contains(u, "/_next/")) {
			seen[u] = true
			jsURLs = append(jsURLs, u)
		}
	}
	texts := fetchAll(jsURLs)

	legacy := false
	for _, u := range jsURLs {
		if strings.Contains(u, "/gd-frontend/") {
			legacy = true
			break
		}
	}

	hashes := map[string]bool{}
	if !legacy { // 新版: sha256(文档原始字符串)
		mods := collectNextfe(texts)
		opRe := regexp.MustCompile(`^\s*(?:query|mutation|subscription)\s+` + regexp.QuoteMeta(operation) + `\b`)
		for id := range mods {
			if doc := resolveNextfe(mods, id, map[string]bool{}); opRe.MatchString(doc) {
				hashes[hashOf(doc)] = true
			}
		}
	} else { // 旧版: sha256(print(addTypename(AST)))
		docs := map[string]*legacyDoc{}
		for _, t := range texts {
			for k, v := range extractLegacyDocs(t) {
				docs[k] = v
			}
		}
		if q := legacyQuery(docs, operation); q != "" {
			hashes[hashOf(q)] = true
		}
	}

	if len(hashes) == 0 {
		return "", fmt.Errorf("operation %s not found", operation)
	}
	out := make([]string, 0, len(hashes))
	for h := range hashes {
		out = append(out, h)
	}
	sort.Strings(out)
	return out[0], nil
}
