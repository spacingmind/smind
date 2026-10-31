package uiview

import (
	"encoding/xml"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxURLBytes caps openUrl.url; 2048 keeps it a sane link, not a payload.
const maxURLBytes = 2048

var (
	// The image and link targets allow one level of nested parentheses, the
	// CommonMark rule, so (alert(1)) in a target cannot leak a stray ")".
	htmlTagRe  = regexp.MustCompile(`<[!/?A-Za-z][^>]*>`)
	openTagRe  = regexp.MustCompile(`<[!/?A-Za-z][^>\n]*$`)
	mdImageRe  = regexp.MustCompile(`!\[([^\]]*)\]\(((?:[^()]|\([^()]*\))*)\)`)
	mdImageRef = regexp.MustCompile(`!\[([^\]]*)\]\[[^\]]*\]`)
	mdRefDefRe = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*(\S+).*$`)
	mdLinkRe   = regexp.MustCompile(`\[([^\]]*)\]\(((?:[^()]|\([^()]*\))*)\)`)

	lucideNameRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,39}$`)
	commandName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	nodeIDRe     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	fieldNameRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,39}$`)
	languageRe   = regexp.MustCompile(`^[A-Za-z0-9+#._-]{1,32}$`)

	svgNamespace = "http://www.w3.org/2000/svg"
	xlinkNS      = "http://www.w3.org/1999/xlink"
)

// SanitizeMarkdown strips raw HTML, keeps images only as their alt text and
// keeps links only when their target is http(s) or mailto. Inline code spans
// and fenced code blocks are left byte-for-byte as the author wrote them, so
// a plugin can show an HTML snippet without the renderer ever seeing it as
// markup. It only ever removes, so the caller's length caps still hold.
func SanitizeMarkdown(s string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if inFence {
			b.WriteString(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = false
			}
		} else if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = true
			b.WriteString(line)
		} else if !unsafeReferenceDefinition(line) {
			writeSanitizedLine(&b, line)
		}
		b.WriteByte('\n')
	}
	out := b.String()
	// The split above always appends a final newline; restore the original
	// ending so a one-line string stays one line.
	if !strings.HasSuffix(s, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out
}

// unsafeReferenceDefinition reports a "[label]: target" definition whose
// target is not http(s) or mailto; "[text][label]" could otherwise turn it
// into a javascript: or data: link. Such a line is dropped whole.
func unsafeReferenceDefinition(line string) bool {
	m := mdRefDefRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	target := strings.ToLower(strings.TrimPrefix(m[1], "<"))
	return !(strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:"))
}

// writeSanitizedLine sanitises one non-fence line, leaving backtick code
// spans untouched.
func writeSanitizedLine(b *strings.Builder, line string) {
	inCode := false
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		if inCode {
			b.WriteString(line[start : i+1])
			start = i + 1
			inCode = false
		} else {
			b.WriteString(sanitizeSegment(line[start:i]))
			start = i
			inCode = true
		}
	}
	// An unclosed backtick opens no code span (CommonMark), so the tail from
	// that backtick on is ordinary text.
	b.WriteString(sanitizeSegment(line[start:]))
}

// sanitizeSegment strips tags and rewrites images and links outside code.
func sanitizeSegment(seg string) string {
	// Repeat so leftovers of nested tricks such as "<scr<script>ipt>" go too.
	for {
		stripped := htmlTagRe.ReplaceAllString(seg, "")
		if stripped == seg {
			break
		}
		seg = stripped
	}
	seg = openTagRe.ReplaceAllString(seg, "")
	seg = mdImageRe.ReplaceAllString(seg, "$1")
	seg = mdImageRef.ReplaceAllString(seg, "$1")
	seg = mdLinkRe.ReplaceAllStringFunc(seg, func(m string) string {
		parts := mdLinkRe.FindStringSubmatch(m)
		target := strings.ToLower(strings.TrimSpace(parts[2]))
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
			return m
		}
		return parts[1]
	})
	return seg
}

// outsideCode removes inline code spans and fenced blocks from s, so tests
// can assert a sanitised result carries no raw HTML anywhere a renderer
// would parse it.
func outsideCode(s string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = false
			}
		} else if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = true
		} else {
			inCode := false
			start := 0
			for i := 0; i < len(line); i++ {
				if line[i] != '`' {
					continue
				}
				if !inCode {
					b.WriteString(line[start:i])
					inCode = true
				} else {
					start = i + 1
					inCode = false
				}
			}
			if !inCode {
				b.WriteString(line[start:])
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// SanitizeSVG validates one inline svg icon under the Paseo rules: one svg
// root, a closed element allowlist, no handlers, no style, fragment-only
// hrefs, no remote or scripted attribute values, no DOCTYPE or entities, and
// text only inside title/desc. A failure is an *Error with Path "svg"; the
// string itself is returned unchanged to the renderer.
func SanitizeSVG(svg string) error {
	if len(svg) > MaxSVGBytes {
		return &Error{"svg", "svg exceeds the 64 KiB cap"}
	}
	dec := xml.NewDecoder(strings.NewReader(svg))
	dec.Strict = true
	var (
		stack     []string
		roots     int
		rootDone  bool
		titleText int
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &Error{"svg", "svg is not well-formed XML: " + err.Error()}
		}
		switch t := tok.(type) {
		case xml.Directive:
			return &Error{"svg", "DOCTYPE and entity declarations are not allowed"}
		case xml.ProcInst:
			return &Error{"svg", "processing instructions are not allowed"}
		case xml.StartElement:
			if rootDone {
				return &Error{"svg", "content follows the root element"}
			}
			if !svgElemSet[t.Name.Local] {
				return &Error{"svg", "element <" + t.Name.Local + "> is not allowed"}
			}
			if t.Name.Space != "" && t.Name.Space != svgNamespace {
				return &Error{"svg", "namespace prefix " + t.Name.Space + " is not allowed"}
			}
			if len(stack) == 0 {
				if t.Name.Local != "svg" {
					return &Error{"svg", "the root element must be <svg>"}
				}
				roots++
			}
			for _, a := range t.Attr {
				if err := checkSVGAttr(a); err != nil {
					return err
				}
			}
			stack = append(stack, t.Name.Local)
		case xml.CharData:
			text := string(t)
			if strings.TrimSpace(text) == "" {
				continue
			}
			if rootDone || len(stack) == 0 {
				return &Error{"svg", "content follows the root element"}
			}
			cur := stack[len(stack)-1]
			if cur != "title" && cur != "desc" {
				return &Error{"svg", "text is only allowed inside <title> and <desc>"}
			}
			titleText += len(text)
			if titleText > 200 {
				return &Error{"svg", "title/desc text exceeds 200 bytes"}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 {
				rootDone = true
			}
		}
	}
	if roots != 1 {
		return &Error{"svg", "exactly one root <svg> element is required"}
	}
	return nil
}

// checkSVGAttr enforces the attribute rules: no on* handlers, no style,
// fragment-only hrefs and no value that names a remote or scripted target.
// xmlns declarations are accepted only for the two standard namespaces,
// which is what keeps the namespaced form of an svg reachable at all.
func checkSVGAttr(a xml.Attr) error {
	local := strings.ToLower(a.Name.Local)
	if strings.HasPrefix(local, "on") {
		return &Error{"svg", "on* handler attributes are not allowed"}
	}
	if local == "style" {
		return &Error{"svg", "style attributes are not allowed"}
	}
	if a.Name.Local == "href" && !strings.HasPrefix(a.Value, "#") {
		return &Error{"svg", "href must reference an id inside the svg (#...)"}
	}
	if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
		if a.Value == svgNamespace || a.Value == xlinkNS {
			return nil
		}
		return &Error{"svg", "namespace declarations other than svg and xlink are not allowed"}
	}
	lower := strings.ToLower(a.Value)
	for _, bad := range []string{"javascript:", "data:", "http:", "https:", "@import"} {
		if strings.Contains(lower, bad) {
			return &Error{"svg", "attribute values may not reference " + bad}
		}
	}
	if i := strings.Index(lower, "url("); i >= 0 && !strings.HasPrefix(lower[i+4:], "#") {
		return &Error{"svg", "url() must reference an id inside the svg (#...)"}
	}
	return nil
}

// sanitizeOpenURL checks an openUrl target and returns the host the confirm
// dialog shows: http/https only, a host, no credentials, no control
// characters or whitespace, at most 2048 bytes. url.Parse lowercases the
// scheme, so HTTPS:// compares equal to https after the case-insensitive
// check here.
func sanitizeOpenURL(raw string) (string, *Error) {
	if len(raw) > maxURLBytes {
		return "", &Error{"", "url exceeds 2048 bytes"}
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return "", &Error{"", "url contains control or whitespace characters"}
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", &Error{"", "url is not valid"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", &Error{"", "scheme must be http or https"}
	}
	if u.Host == "" {
		return "", &Error{"", "url must name a host"}
	}
	if u.User != nil {
		return "", &Error{"", "url must not carry credentials"}
	}
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	return host, nil
}

// validateIcon accepts a Lucide name from the client's registry or a
// sanitized inline svg, and nothing else: no remote image, no name-and-svg.
func validateIcon(icon any, path string) *Error {
	switch t := icon.(type) {
	case string:
		if e := checkString(t, path); e != nil {
			return e
		}
		if !lucideNameRe.MatchString(t) {
			return &Error{path, "icon must be a Lucide name or {svg}"}
		}
	case map[string]any:
		for k := range t {
			if k != "svg" {
				return &Error{path + "." + k, "unknown field"}
			}
		}
		svg, ok := t["svg"].(string)
		if !ok {
			return &Error{path + ".svg", "must be a string"}
		}
		if e := checkString(svg, path+".svg"); e != nil {
			return e
		}
		if err := SanitizeSVG(svg); err != nil {
			return &Error{path + ".svg", err.(*Error).Msg}
		}
	default:
		return &Error{path, "icon must be a Lucide name or {svg}"}
	}
	return nil
}

// validateTone restricts tone to the theme tokens, so a view can never
// carry a raw colour that would break a theme or a colour scheme.
func validateTone(tone any, path string) *Error {
	s, ok := tone.(string)
	if !ok || !toneSet[s] {
		return &Error{path, "tone must be one of success, warning, danger, muted"}
	}
	return nil
}

// checkString enforces the rules every string in a document follows: valid
// UTF-8, no control characters besides newline and tab, at most MaxString
// bytes.
func checkString(s, path string) *Error {
	if !utf8.ValidString(s) {
		return &Error{path, "is not valid UTF-8"}
	}
	if len(s) > MaxString {
		return &Error{path, "exceeds the 8 KiB string cap"}
	}
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return &Error{path, "contains a control character"}
		}
	}
	return nil
}
