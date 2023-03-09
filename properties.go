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

type Properties map[string]any

func (p Properties) Get(k string) any                 { return p[k] }
func (p Properties) GetString(k string) string        { return toString(p[k]) }
func (p Properties) GetInt(k string) int              { return toInt(p[k]) }
func (p Properties) GetFloat64(k string) float64      { return toFloat64(p[k]) }
func (p Properties) GetBool(k string) bool            { return toBool(p[k]) }
func (p Properties) GetStringSlice(k string) []string { return toStringSlice(p[k]) }
func (p Properties) GetAnySlice(k string) []any       { return toAnySlice(p[k]) }

type SliceOfProperties []Properties

func (sp SliceOfProperties) Find(k string, search any) Properties {
	matcher := matcherOrEqual(search)
	for _, p := range sp {
		if matches, _ := matcher.Match(p[k]); matches {
			return p
		}
	}
	return nil
}

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
