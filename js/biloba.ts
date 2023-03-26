type selector = string | string[] | Element

if (!window["_biloba"]) {
    let successResult = (s?, guard?) => (s === undefined || s === null) ? { success: true } : { success: s, guard: guard }
    let errorResult = (err) => { return { success: false, error: err } }
    let resultWrap = (res) => { return { success: true, result: res } }
    let selector = (s: selector) => {
        if (typeof s == "string") {
            if (s.charAt(0) == "x") {
                return document.evaluate(s.slice(1), document, null, XPathResult.ANY_UNORDERED_NODE_TYPE, null).singleNodeValue
            } else {
                return document.querySelector(s.slice(1))
            }
        }
        return s
    }
    let selectorEach = (s: selector) => {
        if (typeof s == "string") {
            if (s.charAt(0) == "x") {
                let xPathResult = document.evaluate(s.slice(1), document, null, XPathResult.UNORDERED_NODE_ITERATOR_TYPE, null)
                const nodes: Node[] = [];
                for (let node = xPathResult.iterateNext(); node != null; node = xPathResult.iterateNext()) nodes.push(node)
                return nodes
            } else {
                return [...document.querySelectorAll(s.slice(1))]
            }
        }
        return s
    }
    let one = (...chain) => (s: selector, ...args) => {
        let n = selector(s)
        let errAnnotation = (typeof s == "string" ? ": " + s.slice(1) : "")
        if (!n) return errorResult("could not find DOM element matching selector" + errAnnotation)
        for (let i = 0; i < chain.length - 1; i++) {
            let r = chain[i](n, ...args)
            if (!r.success) return !!r.error ? r : errorResult(r.guard + errAnnotation)
        }
        let result = chain[chain.length - 1](n, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return result
    }
    let each = (cb) => (s: selector, ...args) => {
        let ns = selectorEach(s)
        let errAnnotation = (typeof s == "string" ? ": " + s.slice(1) : "")

        let result = cb(ns, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return result
    }

    class Biloba {
        exists = s => successResult(!!selector(s))
        count = each(ns => resultWrap(ns.length))
        isVisible = one(n => successResult(n.offsetWidth > 0 || n.offsetHeight > 0 || n.offsetParent != null, "DOM element is not visible"))
        isEnabled = one(n => successResult(!n.disabled, "DOM element is not enabled"))
        click = one(this.isVisible, this.isEnabled, n => successResult(n.click()))
        clickEach = each(ns => {
            ns.forEach(n => this.click(n))
            return successResult()
        })
        getValue = one(n => {
            if (n.type == "checkbox") {
                return resultWrap(n.checked)
            } else if (n.type == "radio") {
                let selected = [...document.querySelectorAll(`input[type="radio"][name="${n.name}"]`)].find(o => (o as HTMLInputElement).checked)
                if (!!selected) return resultWrap((selected as HTMLInputElement).value)
                return resultWrap(null)
            } else if (n.type == "select-multiple") {
                return resultWrap([...n.selectedOptions].map(o => o.value))
            }
            return resultWrap(n.value)
        })
        setValue = one(this.isVisible, this.isEnabled, (n, v: string | string[]) => {
            if (n.type == "select-one" && !n.querySelector(`[value="${v}"]`)) {
                return errorResult(`Select input does not have option with value "${v}"`)
            } else if (n.type == "checkbox") {
                if (typeof v != "boolean") return errorResult("Checkboxes only accept boolean values")
                n.focus()
                n.checked = v
                n.blur()
            } else if (n.type == "radio") {
                if (typeof v != "string") return errorResult("Radio inputs only accept string values")
                let o = document.querySelector(`input[type="radio"][name="${n.name}"][value="${v}"]`) as HTMLInputElement
                if (!o) return errorResult(`Radio input does not have option with value "${v}"`)
                if (!this.isVisible(o).success) return errorResult(`The "${v}" option is not visible`)
                if (!this.isEnabled(o).success) return errorResult(`The "${v}" option is not enabled`)
                o.focus()
                o.checked = true
                o.blur()
                n = o
            } else if (n.type == "select-multiple") {
                if (!Array.isArray(v)) return errorResult("Multi-select inputs only accept []string values")
                let options = [...n.options]
                let optionsToSelect: HTMLOptionElement[] = []
                for (let value of v) {
                    let o = options.find(o => o.value == value) as HTMLOptionElement
                    if (!o) return errorResult(`The "${value}" option does not exist`)
                    if (!this.isEnabled(o).success) return errorResult(`The "${value}" option is not enabled`)
                    optionsToSelect.push(o)
                }
                options.forEach(o => o.selected = false)
                optionsToSelect.forEach(o => o.selected = true)
            } else {
                n.focus()
                n.value = v
                n.blur()
            }
            n.dispatchEvent(new Event('input', { bubbles: true }))
            n.dispatchEvent(new Event('change', { bubbles: true }))
            return successResult()
        })
        hasProperty = one((n, p) => {
            let v = n
            for (const subP of p.split(".")) {
                if (!(subP in v)) return successResult(false)
                v = v[subP]
            }
            return successResult(true)
        })
        eachHasProperty = each((ns, p) => ns.length == 0 ? successResult(false) : successResult(ns.every(n => this.hasProperty(n, p).success)))
        getProperty = one((n, p) => {
            let v = n
            for (const subP of p.split(".")) {
                if (!(subP in v)) return resultWrap(null)
                v = v[subP]
            }
            if (v !== null && v !== undefined && !Array.isArray(v) && (typeof v == "object") && (typeof v[Symbol.iterator] == "function")) {
                v = Array.from(v)
            } else if (v instanceof DOMStringMap) {
                v = { ...v }
            }
            return resultWrap(v)
        })
        getPropertyForEach = each((ns, p) => resultWrap(ns.map(n => this.getProperty(n, p).result)))
        getProperties = one((n, ps) => resultWrap(ps.reduce((m, p) => {
            m[p] = this.getProperty(n, p).result
            return m
        }, {})))
        getPropertiesForEach = each((ns, ps) => resultWrap(ns.map(n => this.getProperties(n, ps).result)))
        setProperty = one((n, p, v) => {
            p = p.split(".")
            for (const subP of p.slice(0, -1)) {
                if (!(subP in n)) return errorResult(`could not resolve property component ".${subP}"`)
                n = n[subP]
            }
            n[p[p.length - 1]] = v
            return successResult()
        })
        setPropertyForEach = each((ns, p, v) => {
            for (const n of ns) {
                let res = this.setProperty(n, p, v)
                if (!res.success) return res
            }
            return successResult()
        })
        invokeOn = one((n, f, ...args) => {
            if (!(f in n) || (typeof n[f] != "function")) return errorResult(`element does not implement "${f}"`)
            return resultWrap(n[f](...args))
        })
        invokeOnEach = each((ns, f, ...args) => resultWrap(ns.map(n => this.invokeOn(n, f, ...args).result)))
        invokeWith = one((n, script, ...args) => resultWrap(eval(script)(n, ...args)))
        invokeWithEach = each((ns, script, ...args) => resultWrap(ns.map(n => this.invokeWith(n, script, ...args).result)))
    }

    window["_biloba"] = new Biloba()
}