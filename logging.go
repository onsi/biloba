package biloba

import (
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/runtime"
)

func (b *Biloba) handleEventConsoleAPICalled(ev *runtime.EventConsoleAPICalled) {
	var color string
	showStackTrace := ev.StackTrace != nil && len(ev.StackTrace.CallFrames) > 1
	switch ev.Type {
	case runtime.APITypeLog:
		color = "{{/}}"
		showStackTrace = false
	case runtime.APITypeDebug:
		color = "{{light-gray}}"
		showStackTrace = false
	case runtime.APITypeInfo:
		color = "{{cyan}}"
		showStackTrace = false
	case runtime.APITypeError:
		color = "{{red}}"
		showStackTrace = showStackTrace && true
	case runtime.APITypeWarning:
		color = "{{coral}}"
		showStackTrace = showStackTrace && true
	case runtime.APITypeAssert:
		color = "{{red}}{{bold}}"
		showStackTrace = showStackTrace && true
	default:
		return
	}
	out := []string{}
	length := 0
	for _, arg := range ev.Args {
		rendition := b.renderRemoteObject(arg)
		out = append(out, rendition)
		length += len(rendition)
	}
	message := ""
	if length > 80 {
		message = out[0] + "\n"
		for _, component := range out[1:] {
			message += b.gt.Fiw(5, 80, "%s\n", component)
		}
	} else {
		message = strings.Join(out, " - ") + "\n"
	}
	b.gt.Printf(b.gt.F("{{gray}}[%s] "+color+"%s{{/}}", ev.Timestamp.Time().Format("15:04"), message))
	if showStackTrace {
		b.gt.Printf(b.gt.Fi(1, b.renderStackTrace(ev.StackTrace)))
	}
	if ev.Type == runtime.APITypeAssert {
		defer func() { recover() }()
		b.gt.Fatalf("Detected console.assert failure:\n%s", message)
	}
}

func (b *Biloba) renderRemoteObject(obj *runtime.RemoteObject) string {
	if len(obj.Value) > 0 {
		return string(obj.Value)
	} else {
		out := ""
		if obj.Preview == nil {
			out += "<nil>"
		} else if obj.Preview.Subtype == "array" {
			out += "["
			for i, property := range obj.Preview.Properties {
				out += fmt.Sprintf("%v", property.Value)
				if i < len(obj.Preview.Properties)-1 {
					out += ", "
				}
			}
			out += "]"
		} else {
			out += "{"
			for i, property := range obj.Preview.Properties {
				out += fmt.Sprintf("%s: %s", property.Name, property.Value)
				if i < len(obj.Preview.Properties)-1 {
					out += ", "
				}
			}
			out += "}"
		}
		return out
	}
}

func (b *Biloba) renderStackTrace(stackTrace *runtime.StackTrace) string {
	out := "{{bold}}Stack Trace{{/}}\n"
	for _, frame := range stackTrace.CallFrames {
		out += fmt.Sprintf("%s {{gray}}%s:%d{{/}}\n", frame.FunctionName, frame.URL, frame.LineNumber)
	}
	return out
}
