package biloba

func newProperties(input any) Properties {
	x := input.(map[string]any)
	return x
}

func newSliceOfProperties(input []any) SliceOfProperties {
	out := make(SliceOfProperties, len(input))
	for i, input := range input {
		out[i] = newProperties(input)
	}
	return out
}

/*
Properties is returned by [Biloba.GetProperties] and provides getters that perform type assertions and nil-conversions for you

For each of the Get* methods if the requested property does not exist or is nil, the getter will return the zero value of the associated type (or an empty slice of correct type for the Get*Slice methods)

Read https://onsi.github.io/biloba/#properties to learn more about properties
*/
type Properties map[string]any

func (p Properties) Get(k string) any                 { return p[k] }
func (p Properties) GetString(k string) string        { return toString(p[k]) }
func (p Properties) GetInt(k string) int              { return toInt(p[k]) }
func (p Properties) GetFloat64(k string) float64      { return toFloat64(p[k]) }
func (p Properties) GetBool(k string) bool            { return toBool(p[k]) }
func (p Properties) GetStringSlice(k string) []string { return toStringSlice(p[k]) }
func (p Properties) GetAnySlice(k string) []any       { return toAnySlice(p[k]) }

/*
SliceOfProperties has underlying type []Properties and is returned by [GetPropertiesForEach].  SliceOfProperties provides getters that perform type assertions and nil-conversions for you.

For each of the Get* methods the return value is a slice of appropriate type. The slice will have the same length as the SliceOfProperties instance and will be filled with the values returned by each Properties invocation of Get* (i.e. nil and missing types will return the zero value.)

Read https://onsi.github.io/biloba/#properties to learn more about properties
*/
type SliceOfProperties []Properties

/*
Find() the first Properties in SliceOfProperties that satisfies the provided search criteria.

If search is a a Gomega matcher then the return Properties will have a value for key k that is successfully matched by the matcher.

# If search is not a matcher then Equal(search) is used to perform the match

Read https://onsi.github.io/biloba/#properties to learn more about properties
*/
func (sp SliceOfProperties) Find(k string, search any) Properties {
	matcher := matcherOrEqual(search)
	for _, p := range sp {
		if matches, _ := matcher.Match(p[k]); matches {
			return p
		}
	}
	return nil
}

/*
Filter() returns the subset of Properties in SliceOfProperties that satisfies the provided search criteria.

The search behaves similarly to [Biloba.Find].

Read https://onsi.github.io/biloba/#properties to learn more about properties
*/
func (sp SliceOfProperties) Filter(k string, search any) SliceOfProperties {
	out := SliceOfProperties{}
	matcher := matcherOrEqual(search)
	for _, p := range sp {
		if matches, _ := matcher.Match(p[k]); matches {
			out = append(out, p)
		}
	}
	return out
}

func (sp SliceOfProperties) Get(k string) []any {
	out := make([]any, len(sp))
	for i, p := range sp {
		out[i] = p.Get(k)
	}
	return out
}
func (sp SliceOfProperties) GetString(k string) []string {
	out := make([]string, len(sp))
	for i, p := range sp {
		out[i] = p.GetString(k)
	}
	return out
}
func (sp SliceOfProperties) GetInt(k string) []int {
	out := make([]int, len(sp))
	for i, p := range sp {
		out[i] = p.GetInt(k)
	}
	return out
}
func (sp SliceOfProperties) GetFloat64(k string) []float64 {
	out := make([]float64, len(sp))
	for i, p := range sp {
		out[i] = p.GetFloat64(k)
	}
	return out
}
func (sp SliceOfProperties) GetBool(k string) []bool {
	out := make([]bool, len(sp))
	for i, p := range sp {
		out[i] = p.GetBool(k)
	}
	return out
}
func (sp SliceOfProperties) GetStringSlice(k string) [][]string {
	out := make([][]string, len(sp))
	for i, p := range sp {
		out[i] = p.GetStringSlice(k)
	}
	return out
}
func (sp SliceOfProperties) GetAnySlice(k string) [][]any {
	out := make([][]any, len(sp))
	for i, p := range sp {
		out[i] = p.GetAnySlice(k)
	}
	return out
}
func toString(input any) string {
	if input == nil {
		return ""
	}
	return input.(string)
}
func toBool(input any) bool {
	if input == nil {
		return false
	}
	return input.(bool)
}
func toInt(input any) int {
	if input == nil {
		return 0
	}
	return int(input.(float64))
}
func toFloat64(input any) float64 {
	if input == nil {
		return 0
	}
	return input.(float64)
}
func toAnySlice(input any) []any {
	if input == nil {
		return []any{}
	}
	return input.([]any)
}
func toStringSlice(input any) []string {
	if input == nil {
		return []string{}
	}
	vs := input.([]any)
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = toString(v)
	}
	return out
}
