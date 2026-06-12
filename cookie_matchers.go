package biloba

import (
	"fmt"
	"strings"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
CookieMatcher is a chainable Gomega matcher returned by [Biloba.HaveCookie].  It passes if the tab has a cookie whose name (and every refined field) matches.  Refinements are added with the WithX methods and all apply to the same cookie:

	Expect(b).To(b.HaveCookie("session").WithValue("abc123").WithPath("/"))

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
type CookieMatcher struct {
	nameMatcher     types.GomegaMatcher
	fieldMatchers   []cookieFieldMatcher
	matchingCookies []Cookie
	allCookies      []Cookie
}

type cookieFieldMatcher struct {
	field   string
	matcher types.GomegaMatcher
	value   func(Cookie) any
}

/*
HaveCookie() returns a [CookieMatcher] that passes if the tab passed to the assertion has a cookie whose name matches.  name may be a string (exact match) or a Gomega matcher:

	Eventually(b).Should(b.HaveCookie("session"))
	Expect(b).To(b.HaveCookie(ContainSubstring("my_guid")))

Chain WithValue/WithPath/WithDomain/WithSameSite/WithSecure/WithHTTPOnly to further constrain the same cookie:

	Expect(b).To(b.HaveCookie("session").WithValue("abc123").WithPath("/").WithSecure())

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) HaveCookie(name any) *CookieMatcher {
	return &CookieMatcher{
		nameMatcher: matcherOrEqual(name),
	}
}

/*
WithValue() refines the [CookieMatcher] to also require the cookie's Value to match.  value may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (m *CookieMatcher) WithValue(value any) *CookieMatcher {
	return m.withField("Value", value, func(c Cookie) any { return c.Value })
}

/*
WithPath() refines the [CookieMatcher] to also require the cookie's Path to match.  path may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (m *CookieMatcher) WithPath(path any) *CookieMatcher {
	return m.withField("Path", path, func(c Cookie) any { return c.Path })
}

/*
WithDomain() refines the [CookieMatcher] to also require the cookie's Domain to match.  domain may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (m *CookieMatcher) WithDomain(domain any) *CookieMatcher {
	return m.withField("Domain", domain, func(c Cookie) any { return c.Domain })
}

/*
WithSameSite() refines the [CookieMatcher] to also require the cookie's SameSite attribute to match (one of "Strict", "Lax", or "None").  sameSite may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (m *CookieMatcher) WithSameSite(sameSite any) *CookieMatcher {
	return m.withField("SameSite", sameSite, func(c Cookie) any { return c.SameSite })
}

/*
WithSecure() refines the [CookieMatcher] to also require the cookie's Secure flag.  With no argument it asserts the flag is true; pass a bool to assert a specific value (WithSecure(false) asserts the cookie is not Secure).

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (m *CookieMatcher) WithSecure(expected ...bool) *CookieMatcher {
	want := true
	if len(expected) > 0 {
		want = expected[0]
	}
	return m.withField("Secure", want, func(c Cookie) any { return c.Secure })
}

/*
WithHTTPOnly() refines the [CookieMatcher] to also require the cookie's HTTPOnly flag.  With no argument it asserts the flag is true; pass a bool to assert a specific value (WithHTTPOnly(false) asserts the cookie is not HTTPOnly).

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (m *CookieMatcher) WithHTTPOnly(expected ...bool) *CookieMatcher {
	want := true
	if len(expected) > 0 {
		want = expected[0]
	}
	return m.withField("HTTPOnly", want, func(c Cookie) any { return c.HTTPOnly })
}

func (m *CookieMatcher) withField(field string, expected any, value func(Cookie) any) *CookieMatcher {
	fieldMatchers := make([]cookieFieldMatcher, len(m.fieldMatchers), len(m.fieldMatchers)+1)
	copy(fieldMatchers, m.fieldMatchers)
	fieldMatchers = append(fieldMatchers, cookieFieldMatcher{
		field:   field,
		matcher: matcherOrEqual(expected),
		value:   value,
	})
	return &CookieMatcher{
		nameMatcher:   m.nameMatcher,
		fieldMatchers: fieldMatchers,
	}
}

func (m *CookieMatcher) Match(actual any) (bool, error) {
	b, ok := actual.(*Biloba)
	if !ok {
		return false, fmt.Errorf("HaveCookie must be passed a Biloba tab.  Got:\n%s", format.Object(actual, 1))
	}
	m.allCookies = b.GetCookies()
	m.matchingCookies = nil
	for _, cookie := range m.allCookies {
		nameMatches, err := m.nameMatcher.Match(cookie.Name)
		if err != nil {
			return false, err
		}
		if !nameMatches {
			continue
		}
		m.matchingCookies = append(m.matchingCookies, cookie)
		allFieldsMatch := true
		for _, fm := range m.fieldMatchers {
			matches, err := fm.matcher.Match(fm.value(cookie))
			if err != nil {
				return false, err
			}
			if !matches {
				allFieldsMatch = false
				break
			}
		}
		if allFieldsMatch {
			return true, nil
		}
	}
	return false, nil
}

func (m *CookieMatcher) description() string {
	out := &strings.Builder{}
	fmt.Fprintf(out, "have a cookie with Name matching %s", m.nameMatcher.FailureMessage(""))
	for _, fm := range m.fieldMatchers {
		fmt.Fprintf(out, "\nand %s matching %s", fm.field, fm.matcher.FailureMessage(""))
	}
	return normalizeWhitespace(out.String())
}

func (m *CookieMatcher) presentCookies() string {
	if len(m.allCookies) == 0 {
		return "There were no cookies present on the tab."
	}
	if len(m.matchingCookies) > 0 {
		out := &strings.Builder{}
		out.WriteString("Cookies matching Name were present but did not satisfy the refinements:")
		for _, c := range m.matchingCookies {
			fmt.Fprintf(out, "\n%s", format.Object(c, 1))
		}
		return out.String()
	}
	out := &strings.Builder{}
	out.WriteString("The cookies present on the tab were:")
	for _, c := range m.allCookies {
		fmt.Fprintf(out, "\n%s", format.Object(c, 1))
	}
	return out.String()
}

func (m *CookieMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected the tab to %s.\n%s", m.description(), m.presentCookies())
}

func (m *CookieMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected the tab not to %s, but it did.", m.description())
}

/*
HaveNumCookies() is a Gomega matcher that passes if the number of cookies on the tab matches expected.  expected may be an int (exact match) or a Gomega matcher:

	Expect(b).To(b.HaveNumCookies(2))
	Expect(b).To(b.HaveNumCookies(BeNumerically(">", 0)))

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) HaveNumCookies(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(actual *Biloba) (bool, error) {
		data["Result"] = len(actual.GetCookies())
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveNumCookies:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}
