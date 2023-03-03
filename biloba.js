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
        if (!n) return rErr("could not find DOM node matching selector: " + s.slice(1))
        for (let i = 0; i < chain.length - 1; i++) {
            let r = chain[i](n, ...args)
            if (!r.success) return !!r.error ? r : rErr(r.guard + ": " + s.slice(1))
        }
        return chain[chain.length - 1](n, ...args)
    }
    let dispatchInputChange = (n) => {
        n.dispatchEvent(new Event('input', { bubbles: true }))
        n.dispatchEvent(new Event('change', { bubbles: true }))
    }

    b.exists = s => r(!!sel(s))
    b.isVisible = h(n => r(n.offsetWidth > 0 || n.offsetHeight > 0 || n.offsetParent != null, "DOM node is not visible"))
    b.isEnabled = h(n => r(!n.disabled, "DOM node is not enabled"))
    b.click = h(b.isVisible, b.isEnabled, n => r(n.click()))
    b.getInnerText = h(n => rRes(n.innerText))
    b.getValue = h(n => rRes(n.value))
    b.setValue = h(b.isVisible, b.isEnabled, (n, v) => {
        n.value = v
        return r(dispatchInputChange(n))
    })
    b.isChecked = h(n => r(n.checked, "DOM node is not checked"))
    b.setChecked = h(b.isVisible, b.isEnabled, (n, v) => {
        n.checked = v
        return r(dispatchInputChange(n))
    })
    b.getClassList = h(n => rRes(Array.from(n.classList)))
    b.hasProperty = h((n, p) => r(p in n, "DOM node does not have property " + p))
    b.getProperty = h(b.hasProperty, (n, p) => rRes(n[p]))

    window["_biloba"] = b
}