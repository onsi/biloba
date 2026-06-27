package biloba

import (
	"encoding/json"
	"fmt"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Storage provides typed access to a tab's localStorage or sessionStorage.  You obtain a Storage handle via [Biloba.LocalStorage] or [Biloba.SessionStorage]:

	b.LocalStorage().Set("user", "Joe")
	var user string
	b.LocalStorage().Get("user", &user)

Storage is scoped to the tab it was created on (and, since each tab lives in its own BrowserContextID, to that isolated context).  Like cookies, web storage requires a navigated origin - you must b.Navigate() to a real URL before using storage (about:blank has an opaque origin and cannot hold storage).

# Type handling

Values are JSON-encoded on Set and JSON-decoded on Get.  This means you can round-trip any JSON-serializable Go value (strings, numbers, bools, slices, maps, structs).  Get accepts an optional pointer argument to decode into a specific type (a la json.Unmarshal); without it Get returns the decoded value as type any (so numbers come back as float64).  Values written to storage outside of Biloba (e.g. by the page itself via localStorage.setItem) that are not valid JSON are returned by Get as their raw string.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
type Storage struct {
	b    *Biloba
	name string //"localStorage" or "sessionStorage"
}

/*
LocalStorage() returns a [Storage] handle for interacting with this tab's localStorage.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) LocalStorage() *Storage {
	return &Storage{b: b, name: "localStorage"}
}

/*
SessionStorage() returns a [Storage] handle for interacting with this tab's sessionStorage.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) SessionStorage() *Storage {
	return &Storage{b: b, name: "sessionStorage"}
}

/*
Set() JSON-encodes value and stores it under key.  The spec fails if the value cannot be encoded or if storage is unavailable (e.g. you have not navigated to a real origin):

	b.LocalStorage().Set("count", 3)
	b.LocalStorage().Set("user", map[string]any{"name": "Joe"})

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (s *Storage) Set(key string, value any) {
	s.b.gt.Helper()
	s.b.guardConfig(s.name + ".Set")
	encoded, err := json.Marshal(value)
	if err != nil {
		s.b.gt.Fatalf("Failed to encode value for %s key %q:\n%s", s.name, key, err.Error())
		return
	}
	setter := s.b.JSFunc(fmt.Sprintf("(k, v) => window.%s.setItem(k, v)", s.name))
	s.b.run(setter.Invoke(key, string(encoded)))
}

/*
Get() reads the value stored under key.  Values are JSON-decoded; pass an optional pointer to decode into a specific type:

	var count int
	b.LocalStorage().Get("count", &count)

	user := b.LocalStorage().Get("user") //returns type any

If key is not present Get returns nil (and leaves the pointer untouched).

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (s *Storage) Get(key string, args ...any) any {
	s.b.gt.Helper()
	getter := s.b.JSFunc(fmt.Sprintf("(k) => window.%s.getItem(k)", s.name))
	var raw any
	s.b.run(getter.Invoke(key), &raw)
	if raw == nil {
		return nil
	}
	rawString, ok := raw.(string)
	if !ok {
		s.b.gt.Fatalf("Unexpected non-string value returned from %s for key %q", s.name, key)
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(rawString), &decoded); err != nil {
		//the stored value isn't valid JSON (e.g. it was written directly by the page) - return it raw
		decoded = rawString
	}
	if len(args) > 0 {
		if err := json.Unmarshal([]byte(rawString), args[0]); err != nil {
			s.b.gt.Fatalf("Failed to decode %s value for key %q:\n%s", s.name, key, err.Error())
			return nil
		}
		return args[0]
	}
	return decoded
}

/*
GetAll() returns all key/value pairs in this storage as a map.  Each value is JSON-decoded (so numbers come back as float64); values that are not valid JSON are returned as their raw string.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (s *Storage) GetAll() map[string]any {
	s.b.gt.Helper()
	s.b.guardConfig(s.name + ".GetAll")
	var rawMap map[string]string
	s.b.run(fmt.Sprintf("({...window.%s})", s.name), &rawMap)
	out := map[string]any{}
	for k, rawString := range rawMap {
		var decoded any
		if err := json.Unmarshal([]byte(rawString), &decoded); err != nil {
			decoded = rawString
		}
		out[k] = decoded
	}
	return out
}

/*
Remove() removes the value stored under key.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (s *Storage) Remove(key string) {
	s.b.gt.Helper()
	s.b.guardConfig(s.name + ".Remove")
	remover := s.b.JSFunc(fmt.Sprintf("(k) => window.%s.removeItem(k)", s.name))
	s.b.run(remover.Invoke(key))
}

/*
Clear() removes all values from this storage.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (s *Storage) Clear() {
	s.b.gt.Helper()
	s.b.guardConfig(s.name + ".Clear")
	s.b.run(fmt.Sprintf("window.%s.clear()", s.name))
}

/*
Length() returns the number of keys in this storage.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (s *Storage) Length() int {
	s.b.gt.Helper()
	s.b.guardConfig(s.name + ".Length")
	var length int
	s.b.run(fmt.Sprintf("window.%s.length", s.name), &length)
	return length
}

func haveStorageItem(name string, get func(*Biloba) *Storage, key string, expected ...any) types.GomegaMatcher {
	var data = map[string]any{}
	data["Name"] = name
	data["Key"] = key
	if len(expected) == 0 {
		return gcustom.MakeMatcher(func(actual *Biloba) (bool, error) {
			_, found := get(actual).GetAll()[key]
			return found, nil
		}).WithTemplate("Expected {{.Data.Name}} {{.To}} have item with key \"{{.Data.Key}}\"", data)
	}
	var matcher = matcherOrEqual(expected[0])
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(actual *Biloba) (bool, error) {
		all := get(actual).GetAll()
		value, found := all[key]
		data["Found"] = found
		data["Result"] = value
		if !found {
			return false, nil
		}
		return matcher.Match(value)
	}).WithTemplate("{{.Data.Name}} item \"{{.Data.Key}}\":\n{{if not .Data.Found}}Expected {{.Data.Name}} to have an item with key \"{{.Data.Key}}\"{{else if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
HaveLocalStorageItem() is a Gomega matcher that operates against the tab passed to the assertion.  With one argument it passes if key exists in localStorage; with a second argument it passes if the stored value matches.  expected may be a string (exact match) or a Gomega matcher:

	Expect(b).To(b.HaveLocalStorageItem("user"))
	Expect(b).To(b.HaveLocalStorageItem("user", "Joe"))
	Eventually(b).Should(b.HaveLocalStorageItem("count", BeNumerically(">", 0)))

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) HaveLocalStorageItem(key string, expected ...any) types.GomegaMatcher {
	return haveStorageItem("localStorage", func(tab *Biloba) *Storage { return tab.LocalStorage() }, key, expected...)
}

/*
HaveSessionStorageItem() is a Gomega matcher that operates against the tab passed to the assertion.  With one argument it passes if key exists in sessionStorage; with a second argument it passes if the stored value matches.  expected may be a string (exact match) or a Gomega matcher:

	Expect(b).To(b.HaveSessionStorageItem("user"))
	Expect(b).To(b.HaveSessionStorageItem("user", "Joe"))

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) HaveSessionStorageItem(key string, expected ...any) types.GomegaMatcher {
	return haveStorageItem("sessionStorage", func(tab *Biloba) *Storage { return tab.SessionStorage() }, key, expected...)
}

func haveNumStorageItems(name string, get func(*Biloba) *Storage, expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(actual *Biloba) (bool, error) {
		data["Result"] = get(actual).Length()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveNum"+name+"Items:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
HaveNumLocalStorageItems() is a Gomega matcher that passes if the number of items in localStorage matches expected.  expected may be an int (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) HaveNumLocalStorageItems(expected any) types.GomegaMatcher {
	return haveNumStorageItems("LocalStorage", func(tab *Biloba) *Storage { return tab.LocalStorage() }, expected)
}

/*
HaveNumSessionStorageItems() is a Gomega matcher that passes if the number of items in sessionStorage matches expected.  expected may be an int (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#cookies-and-storage to learn more about cookies and storage
*/
func (b *Biloba) HaveNumSessionStorageItems(expected any) types.GomegaMatcher {
	return haveNumStorageItems("SessionStorage", func(tab *Biloba) *Storage { return tab.SessionStorage() }, expected)
}
