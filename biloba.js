if (!window["_biloba"]) {
    let b = {}

    let r = (s, guard) => (s === undefined || s === null) ? { success: true } : { success: s, guard: guard }
    let rErr = (err) => { return { error: err } }
    let rRes = (res) => { return { success: true, result: res } }
    let sel = (s) => {
        if (typeof s == "string") {
            if (s.charAt(0) == "x") {
                return document.evaluate(s.slice(1), document, null, XPathResult.ANY_UNORDERED_NODE_TYPE, null).singleNodeValue
            } else {
                return document.querySelector(s.slice(1))
            }
        }
        return s
    }

    let h = (...chain) => (s, ...args) => {
        let n = sel(s)
        if (!n) return rErr("could not find DOM element matching selector: " + s.slice(1))
        for (let i = 0; i < chain.length - 1; i++) {
            let r = chain[i](n, ...args)
            if (!r.success) return !!r.error ? r : rErr(r.guard + ": " + s.slice(1))
        }
        let result = chain[chain.length - 1](n, ...args)
        if (!!result.error) result.error = result.error + ": " + s.slice(1)
        return result
    }
    let dispatchInputChange = (n) => {
        n.dispatchEvent(new Event('input', { bubbles: true }))
        n.dispatchEvent(new Event('change', { bubbles: true }))
    }

    b.exists = s => r(!!sel(s))
    b.isVisible = h(n => r(n.offsetWidth > 0 || n.offsetHeight > 0 || n.offsetParent != null, "DOM element is not visible"))
    b.isEnabled = h(n => r(!n.disabled, "DOM element is not enabled"))
    b.click = h(b.isVisible, b.isEnabled, n => r(n.click()))
    b.getInnerText = h(n => rRes(n.innerText))
    b.getValue = h(n => {
        if (n.type == "checkbox") {
            return rRes(n.checked)
        } else if (n.type == "radio") {
            let selected = [...document.querySelectorAll(`input[type="radio"][name="${n.name}"]`)].find(o => o.checked)
            if (!!selected) return rRes(selected.value)
            return rRes(null)
        } else if (n.type == "select-multiple") {
            return rRes([...n.selectedOptions].map(o => o.value))
        }
        return rRes(n.value)
    })
    b.setValue = h(b.isVisible, b.isEnabled, (n, v) => {
        if (n.type == "select-one" && !n.querySelector(`[value="${v}"]`)) {
            return rErr(`Select input does not have option with value "${v}"`)
        } else if (n.type == "checkbox") {
            if (typeof v != "boolean") return rErr("Checkboxes only accept boolean values")
            n.checked = v
        } else if (n.type == "radio") {
            if (typeof v != "string") return rErr("Radio inputs only accept string values")
            let o = document.querySelector(`input[type="radio"][name="${n.name}"][value="${v}"]`)
            if (!o) return rErr(`Radio input does not have option with value "${v}"`)
            if (!b.isVisible(o).success) return rErr(`The "${v}" option is not visible`)
            if (!b.isEnabled(o).success) return rErr(`The "${v}" option is not enabled`)
            o.checked = true
            return r(dispatchInputChange(o))
        } else if (n.type == "select-multiple") {
            if (!Array.isArray(v)) return rErr("Multi-select inputs only accept []string values")
            let options = [...n.options]
            let optionsToSelect = []
            for (value of v) {
                let o = options.find(o => o.value == value)
                if (!o) return rErr(`The "${value}" option does not exist`)
                if (!b.isEnabled(o).success) return rErr(`The "${value}" option is not enabled`)
                optionsToSelect.push(o)
            }
            options.forEach(o => o.selected = false)
            optionsToSelect.forEach(o => o.selected = true)
        } else {
            n.value = v
        }
        return r(dispatchInputChange(n))
    })
    b.getClassList = h(n => rRes(Array.from(n.classList)))
    b.hasProperty = h((n, p) => {
        let v = n
        for (const subP of p.split(".")) {
            if (!(subP in v)) return r(false, `DOM element does not have property "${p}"`)
            v = v[subP]
        }
        return r(true)
    })
    b.getProperty = h(b.hasProperty, (n, p) => rRes(p.split(".").reduce((v, subP) => v[subP], n)))
    b.setProperty = h((n, p, v) => {
        p = p.split(".")
        for (const subP of p.slice(0, -1)) {
            if (!(subP in n)) return rErr(`could not resolve property component ".${subP}"`)
            n = n[subP]
        }
        console.log("setting", p[p.length - 1], "to", v)
        n[p[p.length - 1]] = v
        return r()
    })

    window["_biloba"] = b
}