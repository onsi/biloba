package biloba_test

import (
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Properties", Label("no-browser"), func() {
	DescribeTable("Properties and SliceOfProperties", func(pName string, fName string, key string, expected any) {
		var p any
		if pName == "properties" {
			p = biloba.Properties{
				"string":      "hello",
				"number":      17.3,
				"bool":        true,
				"stringSlice": []any{"hi", "there", nil},
				"anySlice":    []any{3, true, nil, "hi"},
				"nil":         nil,
			}
		} else if pName == "slice" {
			p = biloba.SliceOfProperties{
				{"string": "hello",
					"number":      17.3,
					"bool":        true,
					"stringSlice": []any{"hi", "there", nil},
					"anySlice":    []any{3, true, nil, "hi"},
					"other":       "fred",
					"nil":         nil},
				{"string": "goodbye",
					"number":      -12.4,
					"bool":        true,
					"stringSlice": []any{"farewell", nil, "friend"},
					"anySlice":    []any{4, nil, "bye", false},
					"other":       nil,
					"nil":         nil},
			}

		} else {
			Fail("Invalid fixture: " + pName)
		}
		val := reflect.ValueOf(p).MethodByName(fName).Call([]reflect.Value{reflect.ValueOf(key)})[0].Interface()
		if expected == nil {
			Expect(val).To(BeNil())
		} else {
			Expect(val).To(Equal(expected))
		}
	}, func(_ any, fName string, key string, expected any) string {
		return fmt.Sprintf("%s(%s) => %v", fName, key, expected)
	},
		Entry(nil, "properties", "Get", "string", "hello"),
		Entry(nil, "properties", "Get", "number", 17.3),
		Entry(nil, "properties", "Get", "bool", true),
		Entry(nil, "properties", "Get", "stringSlice", []any{"hi", "there", nil}),
		Entry(nil, "properties", "Get", "anySlice", []any{3, true, nil, "hi"}),
		Entry(nil, "properties", "Get", "nil", nil),
		Entry(nil, "properties", "Get", "missing", nil),
		Entry(nil, "properties", "GetString", "string", "hello"),
		Entry(nil, "properties", "GetString", "nil", ""),
		Entry(nil, "properties", "GetString", "missing", ""),
		Entry(nil, "properties", "GetInt", "number", 17),
		Entry(nil, "properties", "GetInt", "nil", 0),
		Entry(nil, "properties", "GetFloat64", "number", 17.3),
		Entry(nil, "properties", "GetFloat64", "nil", 0.0),
		Entry(nil, "properties", "GetBool", "bool", true),
		Entry(nil, "properties", "GetBool", "nil", false),
		Entry(nil, "properties", "GetStringSlice", "stringSlice", []string{"hi", "there", ""}),
		Entry(nil, "properties", "GetStringSlice", "nil", []string{}),
		Entry(nil, "properties", "GetAnySlice", "anySlice", []any{3, true, nil, "hi"}),
		Entry(nil, "properties", "GetAnySlice", "nil", []any{}),
		Entry(nil, "slice", "Get", "string", []any{"hello", "goodbye"}),
		Entry(nil, "slice", "Get", "other", []any{"fred", nil}),
		Entry(nil, "slice", "Get", "missing", []any{nil, nil}),
		Entry(nil, "slice", "GetString", "string", []string{"hello", "goodbye"}),
		Entry(nil, "slice", "GetString", "other", []string{"fred", ""}),
		Entry(nil, "slice", "GetString", "nil", []string{"", ""}),
		Entry(nil, "slice", "GetString", "missing", []string{"", ""}),
		Entry(nil, "slice", "GetInt", "number", []int{17, -12}),
		Entry(nil, "slice", "GetInt", "nil", []int{0, 0}),
		Entry(nil, "slice", "GetFloat64", "number", []float64{17.3, -12.4}),
		Entry(nil, "slice", "GetFloat64", "nil", []float64{0.0, 0.0}),
		Entry(nil, "slice", "GetBool", "bool", []bool{true, true}),
		Entry(nil, "slice", "GetBool", "nil", []bool{false, false}),
		Entry(nil, "slice", "GetStringSlice", "stringSlice", [][]string{{"hi", "there", ""}, {"farewell", "", "friend"}}),
		Entry(nil, "slice", "GetStringSlice", "nil", [][]string{{}, {}}),
		Entry(nil, "slice", "GetAnySlice", "anySlice", [][]any{{3, true, nil, "hi"}, {4, nil, "bye", false}}),
		Entry(nil, "slice", "GetAnySlice", "nil", [][]any{{}, {}}),
	)

	Describe("finding and filtering SliceOfProperties", func() {
		var sp biloba.SliceOfProperties
		BeforeEach(func() {
			sp = biloba.SliceOfProperties{
				{"name": "leia", "age": 25},
				{"name": "george", "age": 18},
				{"name": "bill", "age": 14},
				{"name": "kelly", "age": 14},
				{"name": "lisa", "age": 5},
			}
		})

		It("can find specific elements", func() {
			Ω(sp.Find("name", "kelly")).Should(Equal(sp[3]))
			Ω(sp.Find("age", BeNumerically("<", 15))).Should(Equal(sp[2]))
		})

		It("returns nil if none is found", func() {
			Ω(sp.Find("age", BeNumerically(">", 80))).Should(BeNil())
		})

		It("can filter elements", func() {
			Ω(sp.Filter("age", 14)).Should(ConsistOf(sp[2], sp[3]))
			Ω(sp.Filter("name", ContainSubstring("l"))).Should(ConsistOf(sp[0], sp[2], sp[3], sp[4]))
		})

		It("returns empty if none is found", func() {
			Ω(sp.Filter("age", BeNumerically(">", 80))).Should(BeEmpty())
		})
	})
})
