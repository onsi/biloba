package biloba

import (
	"fmt"
	"strings"
)

type XPath string

func (b *Biloba) XPath(path ...string) XPath {
	if len(path) == 0 {
		return XPath("//*")
	}
	if strings.HasPrefix(path[0], "/") || strings.HasPrefix(path[0], "./") {
		return XPath(path[0])
	}
	return XPath("//" + path[0])
}

func (b *Biloba) RelativeXPath(path ...string) XPath {
	if len(path) == 0 {
		return XPath("./*")
	}
	if strings.HasPrefix(path[0], "/") || strings.HasPrefix(path[0], "./") {
		return XPath(path[0])
	}
	return XPath("./" + path[0])
}

func (b *Biloba) XPredicate() XPath {
	return XPath("")
}

func (x XPath) String() string {
	return string(x)
}

func (x XPath) WithAttr(attr string, value string) XPath {
	return x + XPath("[@"+attr+"='"+value+"']")
}

func (x XPath) WithAttrStartsWith(attr string, value string) XPath {
	return x + XPath("[starts-with(@"+attr+", '"+value+"')]")
}

func (x XPath) WithAttrContains(attr string, value string) XPath {
	return x + XPath("[contains(@"+attr+", '"+value+"')]")
}

func (x XPath) WithText(value string) XPath {
	return x + XPath("[text()='"+value+"']")
}

func (x XPath) WithTextStartsWith(value string) XPath {
	return x + XPath("[starts-with(text(), '"+value+"')]")
}

func (x XPath) WithTextContains(value string) XPath {
	return x + XPath("[contains(text(), '"+value+"')]")
}

func (x XPath) WithID(id string) XPath {
	return x.WithAttr("id", id)
}

func (x XPath) WithClass(class string) XPath {
	return x + XPath("[contains(concat(' ',normalize-space(@class),' '),' "+class+" ')]")
}

func (x XPath) Not(predicate XPath) XPath {
	predicateContent := predicate[1 : len(predicate)-1]
	return x + XPath("[not("+predicateContent+")]")
}

func (x XPath) Or(predicates ...XPath) XPath {
	predicateContents := []string{}
	for _, predicate := range predicates {
		predicateContents = append(predicateContents, string("("+predicate[1:len(predicate)-1]+")"))
	}
	return x + XPath("["+strings.Join(predicateContents, " or ")+"]")
}

func (x XPath) And(predicates ...XPath) XPath {
	predicateContents := []string{}
	for _, predicate := range predicates {
		predicateContents = append(predicateContents, string("("+predicate[1:len(predicate)-1]+")"))
	}
	return x + XPath("["+strings.Join(predicateContents, " and ")+"]")
}

func (x XPath) Child(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("/*")
	}
	return x + XPath("/"+tag[0])
}

func (x XPath) Parent() XPath {
	return x + XPath("/..")
}

func (x XPath) Descendant(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("//*")
	}
	return x + XPath("//"+tag[0])
}

func (x XPath) Ancestor(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("/ancestor-or-self::*")
	}
	return x + XPath("/ancestor-or-self::"+tag[0])
}

func (x XPath) DescendantNotSelf(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("/descendant::*")
	}
	return x + XPath("/descendant::"+tag[0])
}

func (x XPath) AncestorNotSelf(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("/ancestor::*")
	}
	return x + XPath("/ancestor::"+tag[0])
}

func (x XPath) FollowingSibling(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("/following-sibling::*")
	}
	return x + XPath("/following-sibling::"+tag[0])
}

func (x XPath) PrecedingSibling(tag ...string) XPath {
	if len(tag) == 0 {
		return x + XPath("/preceding-sibling::*")
	}
	return x + XPath("/preceding-sibling::"+tag[0])
}

func (x XPath) WithChildMatching(childPath XPath) XPath {
	return x + "[" + childPath + "]"
}

func (x XPath) First() XPath {
	return x + XPath("[1]")
}

func (x XPath) Nth(n int) XPath {
	return x + XPath(fmt.Sprintf("[%d]", n))
}

func (x XPath) Last() XPath {
	return x + XPath("[last()]")
}
