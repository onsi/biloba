if (!window["_biloba"]) {
    let b = {}
    let r = (s, guard) => (s === undefined || s === null) ? { success: true } : { success: s, guard: guard }
    let rErr = (err) => { return { error: err } }
    let rRes = (res) => { return { success: true, result: res } }
    // crossInto descends through one open shadow root or same-origin iframe boundary.
    // Closed shadow roots and cross-origin iframes return null (the element won't be found).
    let crossInto = (el) => {
        if (!el) return null
        if (el.shadowRoot) return el.shadowRoot
        try { return el.contentDocument || null } catch (e) { return null }
    }
    // pierceRoot resolves all but the last ">>>" segment to the shadow/iframe root they live
    // in, returning [root, lastSegment] (or [null, ...] if any boundary can't be crossed).
    let pierceRoot = (css) => {
        let segs = css.split(">>>")
        let ctx = document
        for (let i = 0; i < segs.length - 1; i++) {
            let host = ctx.querySelector(segs[i].trim())
            ctx = crossInto(host)
            if (!ctx) return [null, null]
        }
        return [ctx, segs[segs.length - 1].trim()]
    }
    // ---- role / text / label locators (the "a" selector kind) -------------------------------
    // A pragmatic in-page implementation of ARIA role + accessible-name matching (à la
    // getByRole/getByText/getByLabel). It is NOT the full accname spec: it covers explicit
    // role="" + the common implicit roles, and names from aria-labelledby/aria-label/<label>/
    // alt/placeholder/value/text/figcaption/caption/title. It pierces OPEN shadow roots (closed
    // roots and cross-origin frames are skipped, matching crossInto's conservative behavior).
    let normText = (s) => (s || "").replace(/\s+/g, " ").trim()
    // collectElements returns root's element descendants, descending into open shadow roots.
    // root may be a Document, DocumentFragment (shadowRoot), or Element. When root is an Element
    // it is included in the result. Closed shadow roots are skipped.
    let collectElements = (root) => {
        let out = []
        let walk = (node) => {
            let kids = node.querySelectorAll ? node.querySelectorAll("*") : []
            for (let el of kids) {
                out.push(el)
                if (el.shadowRoot) walk(el.shadowRoot)
            }
        }
        if (root.nodeType === 1) { // Element: include it, then walk its (light + shadow) subtree
            out.push(root)
            if (root.shadowRoot) walk(root.shadowRoot)
        }
        walk(root)
        return out
    }
    let implicitRole = (el) => {
        let tag = el.tagName.toLowerCase()
        if (tag === "a" || tag === "area") return el.hasAttribute("href") ? "link" : ""
        if (tag === "h1" || tag === "h2" || tag === "h3" || tag === "h4" || tag === "h5" || tag === "h6") return "heading"
        if (tag === "input") {
            let t = (el.getAttribute("type") || "text").toLowerCase()
            if (t === "checkbox") return "checkbox"
            if (t === "radio") return "radio"
            if (t === "button" || t === "submit" || t === "reset" || t === "image") return "button"
            if (t === "range") return "slider"
            if (t === "search") return "searchbox"
            if (t === "number") return "spinbutton"
            return "textbox" // text, email, tel, url, password, ...
        }
        let map = { button: "button", select: el.multiple ? "listbox" : "combobox", textarea: "textbox", img: el.getAttribute("alt") === "" ? "presentation" : "img", nav: "navigation", main: "main", header: "banner", footer: "contentinfo", aside: "complementary", form: "form", ul: "list", ol: "list", li: "listitem", table: "table", dialog: "dialog", output: "status", progress: "progressbar" }
        return map[tag] || ""
    }
    let roleOf = (el) => {
        let explicit = (el.getAttribute("role") || "").trim().split(/\s+/)[0]
        return explicit || implicitRole(el)
    }
    let accessibleName = (el) => {
        let labelledby = el.getAttribute("aria-labelledby")
        if (labelledby) {
            let names = labelledby.split(/\s+/).map(id => { let r = el.ownerDocument.getElementById(id); return r ? normText(r.textContent) : "" }).filter(Boolean)
            if (names.length) return names.join(" ")
        }
        let aria = el.getAttribute("aria-label")
        if (aria != null && normText(aria)) return normText(aria)
        if (el.labels && el.labels.length) {
            let n = [...el.labels].map(l => normText(l.textContent)).filter(Boolean).join(" ")
            if (n) return n
        }
        let alt = el.getAttribute("alt")
        if (alt != null && normText(alt)) return normText(alt)
        let tag = el.tagName.toLowerCase()
        if (tag === "input" || tag === "textarea" || tag === "select") {
            let t = (el.getAttribute("type") || "").toLowerCase()
            if ((t === "button" || t === "submit" || t === "reset") && el.value) return normText(el.value)
            let ph = el.getAttribute("placeholder")
            if (ph != null && normText(ph)) return normText(ph)
        }
        if (tag === "figure") {
            let cap = el.querySelector("figcaption")
            if (cap && normText(cap.textContent)) return normText(cap.textContent)
        }
        if (tag === "table") {
            let cap = el.querySelector("caption")
            if (cap && normText(cap.textContent)) return normText(cap.textContent)
        }
        let content = normText(el.textContent)
        if (content) return content
        let title = el.getAttribute("title")
        if (title != null && normText(title)) return normText(title)
        return ""
    }
    // describeEl renders an element the way a person would point at it in a failure message:
    // <div#overlay.modal-scrim>.  Classes are capped so a utility-class soup stays one glance wide.
    let describeEl = (el) => {
        if (!el) return ""
        let out = el.tagName.toLowerCase()
        if (el.id) out += "#" + el.id
        let classes = [...el.classList]
        out += classes.slice(0, 3).map(c => "." + c).join("")
        if (classes.length > 3) out += "…"
        return "<" + out + ">"
    }
    let matchText = (actual, target, mode) => mode === "contains" ? actual.includes(target) : actual === target
    let attrText = (el, attr) => { let v = el.getAttribute(attr); return v == null ? null : normText(v) }
    let headingLevel = (el) => {
        let lvl = el.getAttribute("aria-level")
        if (lvl) { let n = parseInt(lvl, 10); if (!isNaN(n)) return n }
        let m = /^h([1-6])$/.exec(el.tagName.toLowerCase())
        return m ? parseInt(m[1], 10) : null
    }
    let stateHolds = (el, state) => {
        if (state === "checked") return el.checked === true || el.getAttribute("aria-checked") === "true"
        if (state === "disabled") return el.disabled === true || el.getAttribute("aria-disabled") === "true"
        if (state === "expanded") return el.getAttribute("aria-expanded") === "true"
        if (state === "pressed") return el.getAttribute("aria-pressed") === "true"
        if (state === "selected") return el.selected === true || el.getAttribute("aria-selected") === "true"
        return false
    }
    let locate = (q) => {
        // 1. candidate pool, piercing open shadow roots. `within` scopes to descendants of the
        // scope element(s); an unresolved scope matches nothing.
        let pool
        if (q.within) {
            let scopes = selEach(q.within)
            if (!scopes.length) return []
            pool = collectElements(document).filter(el => scopes.some(s => s !== el && s.contains(el)))
        } else {
            pool = collectElements(document)
        }
        // 2. base match set. and/or intersect/union operand sets (preserving pool's document order);
        // the leaf kinds filter the pool by their predicate.
        let matched
        if (q.by === "and") {
            let sets = q.operands.map(op => new Set(selEach(op)))
            matched = pool.filter(el => sets.every(s => s.has(el)))
        } else if (q.by === "or") {
            let sets = q.operands.map(op => new Set(selEach(op)))
            matched = pool.filter(el => sets.some(s => s.has(el)))
        } else if (q.by === "css") {
            matched = pool.filter(el => el.matches(q.value))
        } else if (q.by === "role") {
            matched = pool.filter(el => roleOf(el) === q.role && (!q.nameSet || matchText(accessibleName(el), q.name, q.nameMode)))
        } else if (q.by === "label") {
            matched = pool.filter(el => el.matches("input,select,textarea,button,[contenteditable],[role]") && matchText(accessibleName(el), q.value, q.valueMode))
        } else if (q.by === "text") {
            let m = pool.filter(el => matchText(normText(el.textContent), q.value, q.valueMode))
            matched = m.filter(el => !m.some(other => other !== el && el.contains(other))) // smallest matching element
        } else if (q.by === "placeholder") {
            matched = pool.filter(el => (el.tagName === "INPUT" || el.tagName === "TEXTAREA") && attrText(el, "placeholder") != null && matchText(attrText(el, "placeholder"), q.value, q.valueMode))
        } else if (q.by === "alttext") {
            matched = pool.filter(el => attrText(el, "alt") != null && matchText(attrText(el, "alt"), q.value, q.valueMode))
        } else if (q.by === "title") {
            matched = pool.filter(el => attrText(el, "title") != null && matchText(attrText(el, "title"), q.value, q.valueMode))
        } else if (q.by === "testid") {
            matched = pool.filter(el => el.getAttribute(q.attr || "data-testid") === q.value)
        } else {
            matched = []
        }
        // 3. filters: visible-text and has-descendant, each optionally negated.
        if (q.filters) for (let f of q.filters) {
            if (f.kind === "containsText") {
                matched = matched.filter(el => matchText(normText(el.textContent), f.value, f.mode) !== f.negate)
            } else if (f.kind === "contains") {
                let targets = selEach(f.selector)
                matched = matched.filter(el => targets.some(t => t !== el && el.contains(t)) !== f.negate)
            } else if (f.kind === "within") {
                let scopes = selEach(f.selector)
                matched = matched.filter(el => scopes.some(s => s !== el && s.contains(el)) !== f.negate)
            }
        }
        // 4. heading level, 5. ARIA states, 6. ordinal.
        if (q.level != null) matched = matched.filter(el => headingLevel(el) === q.level)
        if (q.states) for (let st of q.states) matched = matched.filter(el => stateHolds(el, st))
        if (q.nthSet) {
            let i = q.nth === -1 ? matched.length - 1 : q.nth
            return (i >= 0 && i < matched.length) ? [matched[i]] : []
        }
        return matched
    }

    let sel = (s) => {
        if (typeof s == "string") {
            if (s.charAt(0) == "x") {
                return document.evaluate(s.slice(1), document, null, XPathResult.ANY_UNORDERED_NODE_TYPE, null).singleNodeValue
            }
            if (s.charAt(0) == "a") {
                let ns = locate(JSON.parse(s.slice(1)))
                return ns.length ? ns[0] : null
            }
            let css = s.slice(1)
            if (css.includes(">>>")) {
                let [root, last] = pierceRoot(css)
                return root ? root.querySelector(last) : null
            }
            return document.querySelector(css)
        }
        return s
    }
    let selEach = (s) => {
        if (typeof s == "string") {
            if (s.charAt(0) == "x") {
                let xPathResult = document.evaluate(s.slice(1), document, null, XPathResult.UNORDERED_NODE_ITERATOR_TYPE, null)
                const nodes = [];
                for (let node = xPathResult.iterateNext(); node != null; node = xPathResult.iterateNext()) nodes.push(node)
                return nodes
            }
            if (s.charAt(0) == "a") {
                return locate(JSON.parse(s.slice(1)))
            }
            let css = s.slice(1)
            if (css.includes(">>>")) {
                let [root, last] = pierceRoot(css)
                return root ? [...root.querySelectorAll(last)] : []
            }
            return [...document.querySelectorAll(css)]
        }
        return s
    }
    // found rides along on every one()/poll() response: it reports whether the selector resolved on
    // THIS attempt, independent of whether the operation then succeeded.  The Go side folds it into the
    // poll-trajectory recorder so a failure can distinguish "never matched" from "matched, then stopped
    // matching" (the detached-node signature).  Purely diagnostic - nothing branches on it.
    let withFound = (result, found) => { result.found = found; return result }
    // ann renders a selector the way a failure message should show it: the "s"/"x"/"a" encoding prefix
    // dropped, introduced by a colon so it reads as a trailing clause.  A locator ("a") carries its
    // own human rendering in desc - Go's Locator.String() - because its wire form is JSON, and a
    // user should never be shown valueMode/nameSet in a failure message.
    let ann = (s) => {
        if (typeof s != "string") return ""
        if (s.charAt(0) == "a") {
            try { return ": " + (JSON.parse(s.slice(1)).desc || s.slice(1)) } catch (e) { return ": " + s.slice(1) }
        }
        return ": " + s.slice(1)
    }
    // notFound is THE missing-element error - the one one() raises - factored out so the hand-rolled
    // multi-selector probes can raise the identical message.  label names WHICH selector went missing
    // when a handler takes more than one ("other "/"container "), so the failure points somewhere.
    let notFound = (s, label) => rErr("could not find DOM element matching " + (label || "") + "selector" + ann(s))
    let one = (...chain) => (s, ...args) => {
        let n = sel(s)
        let errAnnotation = ann(s)
        if (!n) return withFound(notFound(s), false)
        for (let i = 0; i < chain.length - 1; i++) {
            let r = chain[i](n, ...args)
            if (!r.success) return withFound(!!r.error ? r : rErr(r.guard + errAnnotation), true)
        }
        let result = chain[chain.length - 1](n, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return withFound(result, true)
    }
    let each = (cb) => (s, ...args) => {
        let ns = selEach(s)
        let errAnnotation = ann(s)

        let result = cb(ns, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return result
    }
    // poll is one()'s poll-by-default sibling for the value-extracting getters.  Where one() reports a
    // missing element as an *error* (fail fast), poll reports it as {success:false} with no error so the
    // Go-side matcher RETRIES (Eventually) until the element shows up.  A genuine guard failure or thrown
    // error still surfaces as an error (fail fast).  The final callback may itself return {success:false}
    // (no error) to keep the poll waiting - e.g. a required-but-not-yet-defined property.
    //
    // Reach for poll() only when a MISSING element is genuinely "not ready yet" for the caller.  A handler
    // that backs a MATCHER must use one() instead: Gomega counts an assertion satisfied only when the
    // match result is the desired one AND there is no error, so a silent {success:false} makes
    // ShouldNot(<matcher>) pass instantly against a selector that never matches - a vacuous pass.
    let poll = (...chain) => (s, ...args) => {
        let n = sel(s)
        if (!n) return { success: false, found: false } // not found -> retry, NOT an error
        let errAnnotation = ann(s)
        for (let i = 0; i < chain.length - 1; i++) {
            let r = chain[i](n, ...args)
            if (!r.success) return withFound(!!r.error ? r : rErr(r.guard + errAnnotation), true)
        }
        let result = chain[chain.length - 1](n, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return withFound(result, true)
    }
    // parseNameSpec unwraps a property/attribute name argument: a plain string is REQUIRED (it gates the
    // poll until defined) while {__biloba_allow_missing: "x"} (produced by Go's AllowMissing) is optional
    // (returned as null when absent, never blocking the poll).
    let parseNameSpec = (spec) => (spec && typeof spec == "object" && "__biloba_allow_missing" in spec)
        ? { name: spec.__biloba_allow_missing, required: false }
        : { name: spec, required: true }
    let dispatchInputChange = (n) => {
        n.dispatchEvent(new Event('input', { bubbles: true }))
        n.dispatchEvent(new Event('change', { bubbles: true }))
    }
    b.exists = s => r(!!sel(s))
    b.count = each(ns => rRes(ns.length))
    // distinctCountByAttr backs HaveDistinctCount: the number of DISTINCT values the named attribute
    // takes across all matches (elements lacking the attribute collapse into one `null` bucket).  Use
    // it to dedupe transient double-painted nodes keyed by a stable data-* attribute.
    b.distinctCountByAttr = each((ns, attr) => rRes(new Set(ns.map(n => n.getAttribute(attr))).size))
    b.isVisible = one(n => r(n.offsetWidth > 0 || n.offsetHeight > 0 || n.offsetParent != null, "DOM element is not visible"))
    b.isEnabled = one(n => r(!n.disabled, "DOM element is not enabled"))
    // eachIsVisible/eachIsEnabled fail (not vacuously pass) when no elements match: a "every element
    // satisfies" assertion against an empty set is a silent false-positive.  result carries ns.length
    // so the Go matcher can tell "no elements matched" apart from "some element failed the check".
    b.eachIsVisible = each(ns => { return { success: ns.length > 0 && ns.every(n => b.isVisible(n).success), result: ns.length } })
    b.eachIsEnabled = each(ns => { return { success: ns.length > 0 && ns.every(n => b.isEnabled(n).success), result: ns.length } })
    // pointerOpts builds a MouseEvent init from a pointer options object {ox,oy,hasOffset,shift,...}:
    // the coordinates are the element's center, or its top-left corner plus the offset when one is
    // given, and the modifier flags carry through to shift/ctrl/alt/meta-aware handlers.
    let pointerOpts = (n, o) => {
        let rect = n.getBoundingClientRect()
        let cx = o.hasOffset ? rect.left + o.ox : rect.left + rect.width / 2
        let cy = o.hasOffset ? rect.top + o.oy : rect.top + rect.height / 2
        return {
            bubbles: true, cancelable: true, view: window, clientX: cx, clientY: cy,
            shiftKey: !!o.shift, ctrlKey: !!o.control, altKey: !!o.alt, metaKey: !!o.meta,
        }
    }
    // plainPointer is true when no offset or modifier was requested - the case where a plain click
    // can take the maximally-faithful native element.click() path instead of dispatching synthetics.
    let plainPointer = (o) => !o.hasOffset && !o.shift && !o.control && !o.alt && !o.meta
    let dispatchMouse = (n, types, opts) => types.forEach(t => n.dispatchEvent(new MouseEvent(t, opts)))
    // occludedBy runs isClickable's hit-test (shared hitTest/composedContains) WITHOUT gating on it:
    // it returns a description of whatever is topmost at n's center when that thing is neither n nor
    // inside n, and "" otherwise.  A button whose center is covered by its own <span> label is NOT
    // occluded (composedContains sees it).  This is the diagnostic half of the occlusion tradeoff -
    // plain click stays occlusion-blind by design, but a swallowed click now leaves a trail.  One
    // synchronous elementFromPoint inside the click's own atomic snippet; no extra round-trip.
    let occludedBy = (n) => {
        let rect = n.getBoundingClientRect()
        let cx = rect.left + rect.width / 2, cy = rect.top + rect.height / 2
        if (cx < 0 || cy < 0 || cx > window.innerWidth || cy > window.innerHeight) return ""
        let top = hitTest(n.ownerDocument, cx, cy)
        if (!top || composedContains(n, top)) return ""
        return describeEl(top)
    }
    b.click = one(b.isVisible, b.isEnabled, (n, o) => {
        o = o || {}
        let occluder = occludedBy(n) // measured BEFORE dispatch - the click itself may rewrite the DOM
        if (plainPointer(o)) n.click()
        else dispatchMouse(n, ['mousedown', 'mouseup', 'click'], { ...pointerOpts(n, o), button: 0, buttons: 1 })
        return occluder ? rRes(occluder) : r()
    })
    b.dblClick = one(b.isVisible, b.isEnabled, (n, o) => {
        o = o || {}
        if (plainPointer(o)) {
            n.click()
            n.click()
            n.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true, view: window, detail: 2 }))
            return r()
        }
        let opts = { ...pointerOpts(n, o), button: 0, buttons: 1 }
        dispatchMouse(n, ['mousedown', 'mouseup', 'click', 'mousedown', 'mouseup', 'click'], opts)
        n.dispatchEvent(new MouseEvent('dblclick', { ...opts, detail: 2 }))
        return r()
    })
    b.rightClick = one(b.isVisible, b.isEnabled, (n, o) => {
        o = o || {}
        dispatchMouse(n, ['mousedown', 'mouseup', 'contextmenu'], { ...pointerOpts(n, o), button: 2, buttons: 2 })
        return r()
    })
    b.middleClick = one(b.isVisible, b.isEnabled, (n, o) => {
        o = o || {}
        dispatchMouse(n, ['mousedown', 'mouseup', 'auxclick'], { ...pointerOpts(n, o), button: 1, buttons: 4 })
        return r()
    })
    b.tap = one(b.isVisible, b.isEnabled, (n, o) => {
        o = o || {}
        let opts = pointerOpts(n, o), clientX = opts.clientX, clientY = opts.clientY
        let t = new Touch({ identifier: 0, target: n, clientX, clientY })
        n.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, cancelable: true, view: window, pointerType: 'touch', clientX, clientY }))
        n.dispatchEvent(new TouchEvent('touchstart', { bubbles: true, cancelable: true, view: window, touches: [t], targetTouches: [t], changedTouches: [t] }))
        n.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, cancelable: true, view: window, pointerType: 'touch', clientX, clientY }))
        n.dispatchEvent(new TouchEvent('touchend', { bubbles: true, cancelable: true, view: window, touches: [], targetTouches: [], changedTouches: [t] }))
        n.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window, clientX, clientY }))
        return r()
    })
    b.dragTo = one(b.isVisible, (src, targetSel) => {
        let tgt = sel(targetSel)
        if (!tgt) return rErr("could not find DOM element matching target selector")
        let center = (el) => { let b = el.getBoundingClientRect(); return [b.left + b.width / 2, b.top + b.height / 2] }
        let [sx, sy] = center(src), [tx, ty] = center(tgt)
        let fire = (el, type, x, y, buttons) => {
            let opts = { bubbles: true, cancelable: true, view: window, clientX: x, clientY: y, button: 0, buttons: buttons }
            el.dispatchEvent(new PointerEvent('pointer' + type, opts))
            el.dispatchEvent(new MouseEvent('mouse' + type, opts))
        }
        fire(src, 'down', sx, sy, 1)
        let steps = 5
        for (let i = 1; i <= steps; i++) fire(tgt, 'move', sx + (tx - sx) * i / steps, sy + (ty - sy) * i / steps, 1)
        fire(tgt, 'up', tx, ty, 0)
        return r()
    })
    b.scrollWheel = one(b.isVisible, (n, dx, dy) => {
        let box = n.getBoundingClientRect(), x = box.left + box.width / 2, y = box.top + box.height / 2
        let e = new WheelEvent('wheel', { bubbles: true, cancelable: true, view: window, deltaX: dx, deltaY: dy, clientX: x, clientY: y })
        n.dispatchEvent(e)
        if (!e.defaultPrevented) {
            let scrollable = (el) => {
                let s = getComputedStyle(el)
                return (/(auto|scroll)/.test(s.overflowY) && el.scrollHeight > el.clientHeight) || (/(auto|scroll)/.test(s.overflowX) && el.scrollWidth > el.clientWidth)
            }
            let el = n
            while (el && el != document.body && !scrollable(el)) el = el.parentElement
            if (!el || !scrollable(el)) el = document.scrollingElement
            el.scrollTop += dy
            el.scrollLeft += dx
        }
        return r()
    })
    b.focus = one(b.isVisible, b.isEnabled, n => r(n.focus()))
    b.hover = one(b.isVisible, n => {
        let opts = { bubbles: true, cancelable: true, view: window }
        n.dispatchEvent(new PointerEvent('pointerover', opts))
        n.dispatchEvent(new MouseEvent('mouseover', opts))
        n.dispatchEvent(new PointerEvent('pointerenter', opts))
        n.dispatchEvent(new MouseEvent('mouseenter', opts))
        n.dispatchEvent(new MouseEvent('mousemove', opts))
        return r()
    })
    b.scrollIntoView = one(n => { n.scrollIntoView(); return r() })
    // scrollIntoViewP backs ScrollIntoView: with no options it delegates to the native
    // n.scrollIntoView().  Given a container (WithinScroller) it scrolls THAT element; given a top
    // offset (AtTopOffset) it lands the target `offset` CSS pixels below the scroller's top edge
    // (the "under the sticky header" case) rather than flush at the top.  Instant - no smooth scroll,
    // no stability wait; that is the deliberate fast-track tradeoff.  Like other actions a missing
    // target is a fast-fail error (one()); a requested-but-absent container reports {success:false} so
    // the poll keeps waiting.
    b.scrollIntoViewP = one((n, opts) => {
        opts = opts || {}
        let hasOffset = !!opts.hasOffset, offset = opts.offset || 0
        let scroller
        if (opts.container) {
            scroller = sel(opts.container)
            if (!scroller) return { success: false }
        } else if (!hasOffset) {
            n.scrollIntoView()
            return r()
        } else {
            let scrollable = (el) => { let s = getComputedStyle(el); return /(auto|scroll)/.test(s.overflowY) && el.scrollHeight > el.clientHeight }
            let el = n.parentElement
            while (el && el != document.body && !scrollable(el)) el = el.parentElement
            scroller = (el && scrollable(el)) ? el : document.scrollingElement
        }
        let isRoot = scroller === document.scrollingElement || scroller === document.documentElement || scroller === document.body
        let base = isRoot ? 0 : scroller.getBoundingClientRect().top
        scroller.scrollTop = scroller.scrollTop + (n.getBoundingClientRect().top - base) - offset
        return r()
    })
    // dispatchSelection points window.getSelection() at range and fires mouseup on n so
    // selection-driven UIs (floating "highlight → menu" toolbars and the like) react.  selectionchange
    // fires automatically off the getSelection() mutation; the mouseup is the pragmatic touch that
    // wakes the common mouseup-gated menus.
    let dispatchSelection = (n, range) => {
        let selection = window.getSelection()
        selection.removeAllRanges()
        selection.addRange(range)
        n.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, cancelable: true, view: window }))
        return r()
    }
    b.selectText = one(b.isVisible, (n) => {
        let range = document.createRange()
        range.selectNodeContents(n)
        return dispatchSelection(n, range)
    })
    // selectFlatRange maps flat character offsets [start, end) onto the element's text nodes, builds a
    // Range, and dispatches the selection.  shared by selectRange (offsets) and selectOccurrence (substring).
    let selectFlatRange = (n, start, end) => {
        let total = n.textContent.length
        if (start < 0 || end < start || end > total) return rErr(`selection range [${start}, ${end}] is out of bounds (the element's text is ${total} character(s) long)`)
        // walk the element's text nodes, mapping the flat character offsets onto (node, offset) pairs
        let walker = document.createTreeWalker(n, NodeFilter.SHOW_TEXT), pos = 0, sNode = null, sOff = 0, eNode = null, eOff = 0, node
        while (node = walker.nextNode()) {
            let len = node.textContent.length
            if (sNode === null && start <= pos + len) { sNode = node; sOff = start - pos }
            if (eNode === null && end <= pos + len) { eNode = node; eOff = end - pos; break }
            pos += len
        }
        let range = document.createRange()
        if (sNode === null) { range.selectNodeContents(n) } // empty element: [0,0] collapses onto it
        else { range.setStart(sNode, sOff); range.setEnd(eNode, eOff) }
        return dispatchSelection(n, range)
    }
    b.selectRange = one(b.isVisible, (n, start, end) => selectFlatRange(n, start, end))
    b.selectOccurrence = one(b.isVisible, (n, substring, occurrence) => {
        // find the occurrence-th (1-based) appearance of substring in the element's flat textContent,
        // then select exactly that span via the shared flat-offset mapping.
        if (typeof substring != "string" || substring.length == 0) return rErr("substring must be a non-empty string")
        if (typeof occurrence != "number" || occurrence < 1) return rErr("occurrence must be a positive integer")
        let text = n.textContent, start = -1, count = 0, from = 0
        while (true) {
            let idx = text.indexOf(substring, from)
            if (idx < 0) break
            count++
            if (count == occurrence) { start = idx; break }
            from = idx + 1
        }
        if (start < 0) return rErr(`could not find occurrence ${occurrence} of "${substring}" (found ${count} occurrence(s))`)
        return selectFlatRange(n, start, start + substring.length)
    })
    // hitTest pierces open shadow roots: doc.elementFromPoint retargets to the shadow host, so we
    // descend through each host's shadowRoot to find the element actually painted at (x, y). Without
    // this the topmost check below would see the host (not the inner target) and call every element
    // inside an open shadow root obscured.
    let hitTest = (doc, x, y) => {
        let top = doc.elementFromPoint(x, y)
        while (top && top.shadowRoot) {
            let inner = top.shadowRoot.elementFromPoint(x, y)
            if (!inner || inner === top) break
            top = inner
        }
        return top
    }
    // composedContains reports whether `n` is `top` or a flattened-tree ancestor of `top`, walking up
    // across shadow boundaries (Node.contains does not cross them) so the hittability check accepts a
    // target whose painted topmost element lives inside its own shadow tree.
    let composedContains = (n, top) => {
        let node = top
        while (node) {
            if (node === n) return true
            node = node.parentNode || node.host
        }
        return false
    }
    // isClickable is a deterministic, atomic occlusion/hittability check: visible + enabled +
    // the element (or a descendant) is the topmost thing at its own center point. elementFromPoint
    // is synchronous, so this stays in one JS snippet - no async round-trips, no new flakiness.
    // It fails fast (does not wait for animations); that is the deliberate stability tradeoff.
    b.isClickable = one(b.isVisible, b.isEnabled, n => {
        let rect = n.getBoundingClientRect()
        let cx = rect.left + rect.width / 2, cy = rect.top + rect.height / 2
        if (cx < 0 || cy < 0 || cx > window.innerWidth || cy > window.innerHeight) return r(false, "DOM element's center is outside the viewport (it would need to be scrolled into view)")
        let top = hitTest(n.ownerDocument, cx, cy)
        if (!top) return r(false, "DOM element is not hittable at its center point")
        return r(composedContains(n, top), "DOM element is obscured by another element")
    })
    // measurePoint reports an element's centroid in TOP-LEVEL viewport coordinates (where CDP mouse
    // events live), plus whether that point is in the viewport, is hittable (the element/descendant
    // is topmost there), and whether the element is enabled.  It does NOT scroll - callers scroll
    // first.  Coordinates from inside a same-origin iframe are translated by walking up the
    // frameElement chain; the hit-test runs in the element's own document with its local coords.
    let measurePoint = (n) => {
        let doc = n.ownerDocument, view = doc.defaultView
        let rect = n.getBoundingClientRect()
        // clamp the click point to the part of the element that's inside the viewport, so an element
        // larger than the viewport (whose geometric center is off-screen) is still clicked at a
        // visible point.  For a fully-visible element this is just the element's center.
        let vx0 = Math.max(rect.left, 0), vy0 = Math.max(rect.top, 0)
        let vx1 = Math.min(rect.right, view.innerWidth), vy1 = Math.min(rect.bottom, view.innerHeight)
        let inLocalViewport = vx1 > vx0 && vy1 > vy0
        let lx = inLocalViewport ? (vx0 + vx1) / 2 : rect.left + rect.width / 2 // local to the element's own document
        let ly = inLocalViewport ? (vy0 + vy1) / 2 : rect.top + rect.height / 2
        let top = inLocalViewport ? hitTest(doc, lx, ly) : null
        let hittable = !!top && composedContains(n, top)
        let cx = lx, cy = ly, translatable = inLocalViewport
        try {
            while (view && view.frameElement) {
                let fe = view.frameElement, fr = fe.getBoundingClientRect()
                cx += fr.left + fe.clientLeft
                cy += fr.top + fe.clientTop
                view = view.parent
            }
        } catch (e) { translatable = false } // cross-origin frame: cannot translate
        let inViewport = translatable && cx >= 0 && cy >= 0 && cx <= window.innerWidth && cy <= window.innerHeight
        return { x: cx, y: cy, inViewport: inViewport, hittable: hittable, enabled: !n.disabled }
    }
    // docBox reports an element's rectangle in CSS pixels relative to the TOP-LEVEL document (so x/y
    // already include page scroll).  Like measurePoint it walks the frameElement chain so an element
    // inside a same-origin iframe is translated to top-level page coordinates; the final
    // +scrollX/+scrollY converts the top-level viewport rect into document coordinates.  This is the
    // coordinate space page.CaptureScreenshot clips in, which is why both the element-capture clip and
    // the visual-regression mask rectangles are measured with it.
    let docBox = (n) => {
        let rect = n.getBoundingClientRect()
        let left = rect.left, top = rect.top, view = n.ownerDocument.defaultView
        try {
            while (view && view.frameElement) {
                let fe = view.frameElement, fr = fe.getBoundingClientRect()
                left += fr.left + fe.clientLeft
                top += fr.top + fe.clientTop
                view = view.parent
            }
        } catch (e) { } // cross-origin frame: cannot translate; fall back to local coordinates
        let top0 = view || window
        return { x: left + top0.scrollX, y: top + top0.scrollY, width: rect.width, height: rect.height }
    }
    // describeNode names an element the way a failure message should: the tag, plus whichever of id
    // and class actually narrow it down.  Enough for a reader to find the node in their own markup.
    let describeNode = (n) => {
        let s = n.tagName.toLowerCase()
        if (n.id) s += "#" + n.id
        let cls = (n.getAttribute("class") || "").trim()
        if (cls) s += "." + cls.split(/\s+/).slice(0, 3).join(".")
        return s
    }
    // clipperOf finds the ancestor that is CLIPPING an element out of its own capture, and reports how
    // much of the element survives that clip.  This exists because a screenshot can only contain what
    // the browser painted: captureBeyondViewport expands the MAIN FRAME's viewport, so an element
    // below the document fold captures fine, but an element scrolled outside an inner overflow:auto
    // pane was never painted and comes back as a flat rectangle of the pane's background.  A blank
    // capture is stable, so as a baseline it passes forever while comparing nothing.
    //
    // The walk deliberately skips <html>/<body>: the document scroller is the case
    // captureBeyondViewport already handles, and flagging it would fire on every ordinary
    // below-the-fold capture.
    let clipperOf = (n) => {
        let rect = n.getBoundingClientRect()
        let area = rect.width * rect.height
        if (area <= 0) return null
        let el = n.parentElement
        while (el && el !== document.body && el !== document.documentElement) {
            let s = getComputedStyle(el)
            if (/(auto|scroll|hidden|clip)/.test(s.overflowX) || /(auto|scroll|hidden|clip)/.test(s.overflowY)) {
                let r = el.getBoundingClientRect()
                let w = Math.max(0, Math.min(rect.right, r.right) - Math.max(rect.left, r.left))
                let h = Math.max(0, Math.min(rect.bottom, r.bottom) - Math.max(rect.top, r.top))
                let visible = (w * h) / area
                // 0.999 rather than 1: a fractional layout can leave a sub-pixel sliver outside the
                // clip on a box that is, for every purpose anybody cares about, fully visible.
                if (visible < 0.999) return { clipper: describeNode(el), visibleFraction: visible }
            }
            el = el.parentElement
        }
        return null
    }
    // fullyInViewport reports whether an element's box lies entirely within the TOP-LEVEL visual
    // viewport.  This is what decides whether a capture needs Chrome's captureBeyondViewport, which is
    // not free: it drives the layout viewport down and back, and a responsive page OBSERVES that -
    // matchMedia flips, a resize fires, and an app that re-renders on its breakpoint can unmount the
    // very subject being captured.  A subject already in view needs none of it.
    //
    // Conservative on every uncertainty: a cross-origin frame we cannot translate through reports
    // false, which keeps the old always-expand behaviour for the case we cannot reason about.
    let fullyInViewport = (n) => {
        let rect = n.getBoundingClientRect()
        let left = rect.left, top = rect.top, view = n.ownerDocument.defaultView
        try {
            while (view && view.frameElement) {
                let fe = view.frameElement, fr = fe.getBoundingClientRect()
                left += fr.left + fe.clientLeft
                top += fr.top + fe.clientTop
                view = view.parent
            }
            let top0 = view || window
            return left >= 0 && top >= 0 && left + rect.width <= top0.innerWidth && top + rect.height <= top0.innerHeight
        } catch (e) { return false } // cross-origin frame: cannot translate, so assume it is not
    }
    // boundingBox reports the first matching element's clip rectangle for page.CaptureScreenshot,
    // along with the clipping ancestor (if any) that would leave part of that rectangle unpainted and
    // whether the element is already fully in view.  Errors on a zero-area element.
    b.boundingBox = one(n => {
        let box = docBox(n)
        if (box.width <= 0 || box.height <= 0) return rErr("DOM element has zero area")
        let clipped = clipperOf(n)
        if (clipped) { box.clipper = clipped.clipper; box.visibleFraction = clipped.visibleFraction }
        box.inViewport = fullyInViewport(n)
        return rRes(box)
    })
    // fontsReady resolves once the document's web fonts have finished loading.  A capture taken before
    // a font swap lands measures text in the fallback face - which the ASSERT path rides out by
    // re-capturing every poll attempt, but a baseline WRITE cannot, since it passes on the first
    // attempt it settles on.  Async (returns a Promise); invoked with awaitPromise on the Go side.
    b.fontsReady = () => {
        if (!document.fonts || !document.fonts.ready) return Promise.resolve(r())
        return document.fonts.ready.then(() => r(), () => r())
    }
    // maskBoxes reports EVERY element matching ANY of the given selectors, in the same top-level
    // document coordinates boundingBox produces, so Go can paint those regions out of a capture before
    // comparing it against a baseline.  It takes the whole list rather than one selector at a time so
    // b.Mask(a, b, c) measures all three in ONE atomic read of the page - a capture happens on every
    // poll attempt, and three round trips could each see a different frame.  A selector that matches
    // nothing contributes nothing rather than erroring - masking an element that is only sometimes on
    // the page must not fail the assertion - and zero-area elements are dropped since there is nothing
    // to paint over.
    b.maskBoxes = (ss) => rRes(ss.flatMap(s => selEach(s)).map(docBox).filter(x => x.width > 0 && x.height > 0))
    // freezeRendering pins the page in its deterministic un-animated state for a visual capture: CSS
    // animations and transitions off, the blinking text caret invisible, smooth scrolling off.  It is
    // `animation: none` rather than `animation-play-state: paused` on purpose - pausing freezes each
    // animation at whatever frame it happened to reach, which is the nondeterminism we are trying to
    // remove, while none resets to the un-animated rendering.  The stylesheet goes into every OPEN
    // shadow root as well as the document: a `*` rule in a document stylesheet does not apply inside a
    // shadow root, so without this a web component would keep animating - and keep blinking its caret -
    // right through the capture.  Closed shadow roots cannot be reached and are left alone by design.
    // Idempotent, as is unfreezeRendering.
    let freezeCSS = "*, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; scroll-behavior: auto !important; }"
    // freezeRoots is the document plus every open shadow root under it (collectElements already
    // descends open roots, so nested components are included).  ShadowRoot inherits getElementById from
    // DocumentFragment, so the idempotence check below reads the same on either kind of root; only the
    // append target differs.
    let freezeRoots = () => [document, ...collectElements(document).filter(el => el.shadowRoot).map(el => el.shadowRoot)]
    b.freezeRendering = () => {
        for (let root of freezeRoots()) {
            if (root.getElementById("_biloba-freeze")) continue
            let s = document.createElement("style")
            s.id = "_biloba-freeze"
            s.textContent = freezeCSS
            let host = (root === document) ? document.head : root
            host.appendChild(s)
        }
        return rRes(true)
    }
    b.unfreezeRendering = () => {
        for (let root of freezeRoots()) {
            let s = root.getElementById("_biloba-freeze")
            if (s) s.remove()
        }
        return rRes(true)
    }
    // scrollToStablePoint backs single-element realistic interactions: it scrolls the element to the
    // viewport center, waits for its box to stop moving (two consecutive animation frames with the
    // same rect - bounded so a perpetually-animating element can't hang), then returns measurePoint.
    // Async (returns a Promise); invoked with awaitPromise on the Go side.
    b.scrollToStablePoint = (s) => {
        let errAnnotation = ann(s)
        let n = sel(s)
        if (!n) return Promise.resolve(rErr("could not find DOM element matching selector" + errAnnotation))
        if (!b.isVisible(n).success) return Promise.resolve(rErr("DOM element is not visible" + errAnnotation))
        n.scrollIntoView({ block: "center", inline: "center" })
        return new Promise(resolve => {
            let prev = null, frames = 0
            let check = () => {
                let bx = n.getBoundingClientRect()
                let k = [bx.left, bx.top, bx.width, bx.height].join(",")
                if (k === prev || frames++ > 30) resolve(rRes(measurePoint(n)))
                else { prev = k; requestAnimationFrame(check) }
            }
            requestAnimationFrame(check)
        })
    }
    // measureCorner reports an element's top-left corner in TOP-LEVEL viewport coordinates (where CDP
    // mouse events live), plus whether the element is enabled.  Like measurePoint it walks the
    // frameElement chain so a corner inside a same-origin iframe is translated to top-level coords.
    // Callers add their own (offsetX, offsetY) and check the resulting point against the viewport.
    let measureCorner = (n) => {
        let rect = n.getBoundingClientRect()
        let left = rect.left, top = rect.top, view = n.ownerDocument.defaultView, translatable = true
        try {
            while (view && view.frameElement) {
                let fe = view.frameElement, fr = fe.getBoundingClientRect()
                left += fr.left + fe.clientLeft
                top += fr.top + fe.clientTop
                view = view.parent
            }
        } catch (e) { translatable = false } // cross-origin frame: cannot translate
        return { left: left, top: top, translatable: translatable, enabled: !n.disabled, innerWidth: window.innerWidth, innerHeight: window.innerHeight }
    }
    // scrollToStableCorner backs ClickAt in realistic mode: it scrolls the element to the viewport
    // center, waits for its box to stop moving (same stability wait as scrollToStablePoint), then
    // returns its top-left corner in top-level viewport coordinates.  Async (returns a Promise).
    b.scrollToStableCorner = (s) => {
        let errAnnotation = ann(s)
        let n = sel(s)
        if (!n) return Promise.resolve(rErr("could not find DOM element matching selector" + errAnnotation))
        if (!b.isVisible(n).success) return Promise.resolve(rErr("DOM element is not visible" + errAnnotation))
        n.scrollIntoView({ block: "center", inline: "center" })
        return new Promise(resolve => {
            let prev = null, frames = 0
            let check = () => {
                let bx = n.getBoundingClientRect()
                let k = [bx.left, bx.top, bx.width, bx.height].join(",")
                if (k === prev || frames++ > 30) resolve(rRes(measureCorner(n)))
                else { prev = k; requestAnimationFrame(check) }
            }
            requestAnimationFrame(check)
        })
    }
    // scrollToAndPointAt backs realistic ClickEach: scroll+measure the index-th match (no stability
    // wait), or null when it is missing/hidden so the caller can skip it.
    b.scrollToAndPointAt = each((ns, i) => {
        let n = ns[i]
        if (!n || !b.isVisible(n).success) return rRes(null)
        n.scrollIntoView({ block: "center", inline: "center" })
        return rRes(measurePoint(n))
    })
    // inputKind classifies a form control so the realistic track can decide how to drive it.
    b.inputKind = one(n => {
        let t = n.type
        if (t === "checkbox") return rRes("checkbox")
        if (t === "radio") return rRes("radio")
        if (t === "select-one" || t === "select-multiple") return rRes("select")
        return rRes("text")
    })
    b.blur = one(n => r(n.blur()))
    b.node = (s) => sel(s)
    b.clickEach = each(ns => {
        ns.forEach(n => b.click(n))
        return r()
    })
    let getValueImpl = (n) => {
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
    }
    b.getValue = one(getValueImpl)
    b.getValueP = poll(getValueImpl) // GetValue: poll until the element is present ("" is a valid value)
    // getValueForEach backs CurrentValueForEach: a pure snapshot of each match's rationalized value.
    b.getValueForEach = each((ns) => rRes(ns.map(n => getValueImpl(n).result)))
    b.setValue = one(b.isVisible, b.isEnabled, (n, v) => {
        // a ValueLabel argument arrives as {__biloba_value_label: "..."}; labelOf unwraps it (or returns null)
        let labelOf = (val) => (val && typeof val == "object" && "__biloba_value_label" in val) ? val.__biloba_value_label : null
        if (labelOf(v) !== null && n.type != "select-one") {
            return rErr(`ValueLabel is only supported for <select> elements`)
        }
        if (n.type == "select-one") {
            let label = labelOf(v)
            if (label !== null) {
                let o = [...n.options].find(o => o.text == label)
                if (!o) return rErr(`Select input does not have option with label "${label}"`)
                v = o.value
            } else if (!n.querySelector(`[value="${v}"]`)) {
                return rErr(`Select input does not have option with value "${v}"`)
            }
            n.focus()
            n.value = v
            n.blur()
        } else if (n.type == "checkbox") {
            if (typeof v != "boolean") return rErr("Checkboxes only accept boolean values")
            n.focus()
            n.checked = v
            n.blur()
        } else if (n.type == "radio") {
            if (typeof v != "string") return rErr("Radio inputs only accept string values")
            let o = document.querySelector(`input[type="radio"][name="${n.name}"][value="${v}"]`)
            if (!o) return rErr(`Radio input does not have option with value "${v}"`)
            if (!b.isVisible(o).success) return rErr(`The "${v}" option is not visible`)
            if (!b.isEnabled(o).success) return rErr(`The "${v}" option is not enabled`)
            o.focus()
            o.checked = true
            o.blur()
            n = o
        } else if (n.type == "select-multiple") {
            if (!Array.isArray(v)) return rErr("Multi-select inputs only accept []string values")
            let options = [...n.options]
            let optionsToSelect = []
            for (value of v) {
                let label = labelOf(value)
                let o = label !== null ? options.find(o => o.text == label) : options.find(o => o.value == value)
                if (!o) return rErr(`The "${label !== null ? label : value}" option does not exist`)
                if (!b.isEnabled(o).success) return rErr(`The "${label !== null ? label : value}" option is not enabled`)
                optionsToSelect.push(o)
            }
            options.forEach(o => o.selected = false)
            optionsToSelect.forEach(o => o.selected = true)
        } else {
            // set via the native prototype value setter so React/Vue/Solid *controlled* inputs update.
            // these frameworks install a value tracker that shadows the element's own value setter and
            // gates onChange on it; a raw `n.value = v` updates the tracker's cache too, so the dispatched
            // input event looks like a no-op and the framework reconciles the DOM back to its bound state.
            // calling the original prototype setter bypasses the tracker so the change is seen as genuine.
            // we deliberately do NOT blur() here: input+change are dispatched explicitly below, so blurring
            // adds no event semantics for text inputs and would fire onBlur handlers (commit-on-blur, inline
            // edit unmount, ...) as a surprising side effect mid-call.
            n.focus()
            let proto = n instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
            Object.getOwnPropertyDescriptor(proto, 'value').set.call(n, v)
        }
        n.dispatchEvent(new Event('input', { bubbles: true }))
        n.dispatchEvent(new Event('change', { bubbles: true }))
        return r()
    })
    b.getAttribute = one((n, a) => rRes(n.getAttribute(a)))
    b.getAttributeForEach = each((ns, a) => rRes(ns.map(n => n.getAttribute(a))))
    // getAttributesForEach backs CurrentAttributesForEach: a pure snapshot of the named raw HTML
    // attributes for each match.  Absent attributes come back as null (no two-axis polling here).
    b.getAttributesForEach = each((ns, names) => rRes(ns.map(n => names.reduce((m, a) => {
        m[a] = n.getAttribute(a)
        return m
    }, {}))))
    // getAttributesP backs GetAttribute/GetAttributes: poll until the element is present AND every
    // REQUIRED named attribute is present (getAttribute !== null); AllowMissing names never block.
    // When it is NOT ready the response carries a diagnostic ({element, undefined}) instead of a value,
    // so a timeout can say "the element was there the whole time; the ATTRIBUTE never appeared" and
    // name AllowMissing.  {success:false} still means "retry" - nothing branches on the diagnostic.
    b.getAttributesP = poll((n, specs) => {
        let result = {}, ready = true, undef = []
        for (const spec of specs) {
            let { name, required } = parseNameSpec(spec)
            let v = n.getAttribute(name)
            if (required && v === null) { ready = false; undef.push(name) }
            result[name] = v
        }
        return ready ? rRes(result) : { success: false, result: { element: describeEl(n), undefined: undef } }
    })
    b.hasAttribute = one((n, a) => r(n.hasAttribute(a)))
    b.isFocused = one(n => r(n === document.activeElement, "DOM element is not focused"))
    // isChecked backs BeChecked.  An element with no `checked` property AT ALL - the classic "I selected
    // the wrapping <label>/<div>, not the <input>" mistake - is an ERROR, not merely "not checked":
    // reading the missing property and coercing undefined to false would make ShouldNot(b.BeChecked())
    // pass forever against an element that could never be checked.  A real input answers normally,
    // whether or not it is checked.
    b.isChecked = one(n => {
        if (!("checked" in n)) return rErr(`${describeEl(n)} has no "checked" property (did you select the label or wrapper rather than the input?) for selector`)
        return r(n.checked === true, "DOM element is not checked")
    })
    // computedStyleValue resolves a computed CSS property.  getPropertyValue is the canonical resolver -
    // it handles CSS custom properties ("--stage") and kebab-case names ("z-index").  When it yields ""
    // we fall back to camelCase indexing so legacy camelCase names ("backgroundColor") still resolve.
    let computedStyleValue = (n, p) => {
        let cs = window.getComputedStyle(n)
        let v = cs.getPropertyValue(p)
        return (v === "" && (p in cs)) ? cs[p] : v
    }
    b.getComputedStyle = one((n, p) => rRes(computedStyleValue(n, p)))
    // getComputedStyleP backs GetComputedStyle: poll until the element is present, then return the
    // resolved value (custom properties included).
    b.getComputedStyleP = poll((n, p) => rRes(computedStyleValue(n, p)))
    // getComputedStyleNumericP backs GetComputedStyleNumeric/HaveComputedStyleNumeric: once the element
    // is present, return the leading numeric part of the resolved value (parseFloat, so "16px" -> 16,
    // "1.5" -> 1.5).  A non-numeric value ("none", "auto") is a hard error, not a wait-forever, so the
    // caller fails fast with a clear message instead of timing out.  one() (not poll()) so a MISSING
    // element errors: HaveComputedStyleNumeric is a matcher, and its documented sibling
    // HaveComputedStyle reads through the one()-based getComputedStyle - both must reject
    // ShouldNot(...) against an element that never exists rather than pass vacuously.
    b.getComputedStyleNumericP = one((n, p) => {
        let raw = computedStyleValue(n, p)
        let v = parseFloat(raw)
        if (isNaN(v)) return rErr(`computed style "${p}" is "${raw}", which is not numeric`)
        return rRes(v)
    })
    // normalizeColor normalizes any CSS <color> (including a var(--token) chain, which inherits the
    // document's custom properties through a throwaway probe appended to <body>) to the browser's
    // canonical resolved form ("rgb(...)"/"rgba(...)").  It has no selector - callers invoke it
    // directly.  An unparseable color is an error (the browser leaves style.color untouched).
    b.normalizeColor = (input) => {
        let probe = document.createElement("span")
        probe.style.color = input
        if (probe.style.color === "") return rErr(`"${input}" is not a valid CSS color`)
        probe.style.display = "none"
        document.body.appendChild(probe)
        let resolved = getComputedStyle(probe).color
        document.body.removeChild(probe)
        return rRes(resolved)
    }
    b.hasProperty = one((n, p) => {
        let v = n
        for (const subP of p.split(".")) {
            if (!(subP in v)) return r(false)
            v = v[subP]
        }
        return r(true)
    })
    // resolveProperty walks a dot-delimited property path on node n.  found reports whether the whole
    // path resolved (the "defined" axis for two-axis polling); value is the (array/object-normalized)
    // leaf, or null when the path doesn't resolve.
    let resolveProperty = (n, p) => {
        let v = n
        for (const subP of p.split(".")) {
            if (!(subP in v)) return { found: false, value: null }
            v = v[subP]
        }
        if (v !== null && v !== undefined && !Array.isArray(v) && (typeof v == "object") && (typeof v[Symbol.iterator] == "function")) {
            v = Array.from(v)
        } else if (v instanceof DOMStringMap) {
            v = { ...v }
        }
        return { found: true, value: v }
    }
    b.getProperty = one((n, p) => rRes(resolveProperty(n, p).value))
    b.getPropertyForEach = each((ns, p) => rRes(ns.map(n => b.getProperty(n, p).result)))
    // eachHasProperty backs the existence-only form of EachHaveProperty.  It resolves the property on
    // every match itself (rather than delegating to getPropertyForEach) because it needs BOTH axes of
    // resolveProperty: `found` is what distinguishes "undefined" from "defined but null", and that
    // distinction IS the question the existence-only form asks.  result carries {count, values}: count
    // lets the Go matcher fail on an empty set with a clear "no elements matched" message rather than a
    // vacuous pass, and values - the very same []any the value-matching form produces - is what
    // .Capture() hands back.
    b.eachHasProperty = each((ns, p) => {
        let resolved = ns.map(n => resolveProperty(n, p))
        return { success: ns.length > 0 && resolved.every(x => x.found), result: { count: ns.length, values: resolved.map(x => x.value) } }
    })
    b.getProperties = one((n, ps) => rRes(ps.reduce((m, p) => {
        m[p] = b.getProperty(n, p).result
        return m
    }, {})))
    b.getPropertiesForEach = each((ns, ps) => rRes(ns.map(n => b.getProperties(n, ps).result)))
    // getPropertiesP backs GetProperty/GetProperties: poll until the element is present AND every
    // REQUIRED named property is defined (the path resolves); AllowMissing names return null and never
    // block.  The result map is keyed by the plain property name (matching Go's nameOf).
    // When it is NOT ready the response carries a diagnostic ({element, undefined}) instead of a value,
    // so a timeout can say "the element was there the whole time; the PROPERTY never appeared" and
    // name AllowMissing.  {success:false} still means "retry" - nothing branches on the diagnostic.
    b.getPropertiesP = poll((n, specs) => {
        let result = {}, ready = true, undef = []
        for (const spec of specs) {
            let { name, required } = parseNameSpec(spec)
            let resolved = resolveProperty(n, name)
            if (required && !resolved.found) { ready = false; undef.push(name) }
            result[name] = resolved.found ? resolved.value : null
        }
        return ready ? rRes(result) : { success: false, result: { element: describeEl(n), undefined: undef } }
    })
    b.setProperty = one((n, p, v) => {
        p = p.split(".")
        for (const subP of p.slice(0, -1)) {
            if (!(subP in n)) return rErr(`could not resolve property component ".${subP}"`)
            n = n[subP]
        }
        n[p[p.length - 1]] = v
        return r()
    })
    b.setPropertyForEach = each((ns, p, v) => {
        for (const n of ns) {
            let res = b.setProperty(n, p, v)
            if (!res.success) return res
        }
        return r()
    })
    let invokeOnImpl = (n, f, ...args) => {
        if (!(f in n) || (typeof n[f] != "function")) return rErr(`element does not implement "${f}"`)
        return rRes(n[f](...args))
    }
    b.invokeOn = one(invokeOnImpl)
    b.invokeOnP = poll(invokeOnImpl) // InvokeOn: missing element retries; undefined method / throw fail fast
    b.invokeOnEach = each((ns, f, ...args) => rRes(ns.map(n => invokeOnImpl(n, f, ...args).result)))
    let invokeWithImpl = (n, script, ...args) => rRes(eval(script)(n, ...args))
    b.invokeWith = one(invokeWithImpl)
    b.invokeWithP = poll(invokeWithImpl) // InvokeWith: missing element retries; thrown JS fails fast
    b.invokeWithEach = each((ns, script, ...args) => rRes(ns.map(n => invokeWithImpl(n, script, ...args).result)))

    // --- Geometry getters (pollable) ---------------------------------------------------------------
    // Every geometry probe splits ABSENT from NOT-YET-LAID-OUT, and the split is load-bearing:
    //   - element absent          -> an ERROR (the same message one() raises).  These probes back
    //                                matchers, and Gomega only counts an assertion satisfied when the
    //                                match result is the desired one AND there is no error - so without
    //                                the error, ShouldNot(b.BeInViewport()) would pass INSTANTLY against
    //                                an element that never rendered.  The error keeps the negation honest
    //                                in both directions (and, under Eventually, still just retries).
    //   - present but degenerate -> {success:false}, no error - "not ready yet", so the POSITIVE
    //     (a zero-area box)          direction keeps polling through late layout exactly as before.
    // boundingBoxP backs BoundingBox/HaveBoundingBox: wait until the element is present AND has a
    // non-degenerate layout box (width>0 && height>0 - actually laid out, not merely in the DOM), then
    // return its viewport-relative rectangle.
    b.boundingBoxP = one(n => {
        let x = n.getBoundingClientRect()
        if (x.width <= 0 || x.height <= 0) return { success: false }
        return rRes(boxOf(n))
    })
    // scrollOffsetP backs ScrollOffset/HaveScrollOffset: once the (scroll container) element is present,
    // report its current scroll position and the maximum scrollable offsets (scroll size minus client
    // size) so a spec can assert "scrolled to / near the bottom" without hand-rolled JS.
    b.scrollOffsetP = one(n => rRes({ top: n.scrollTop, left: n.scrollLeft, maxTop: n.scrollHeight - n.clientHeight, maxLeft: n.scrollWidth - n.clientWidth }))
    // offsetWithinP backs OffsetTopWithin/OffsetLeftWithin/HaveOffsetTopWithin: wait until BOTH the
    // element and the container are present and the element has a non-degenerate box, then report the
    // element's viewport offset minus the container's - i.e. how far below/right of the container's
    // top-left edge the element currently sits (the "scrolled near the top of the pane" measurement).
    // The container arrives as an already-encoded selector (Go encodes it) so sel() resolves it directly.
    // Either endpoint being absent is an error that names WHICH selector went missing.
    b.offsetWithinP = (s, containerSel) => {
        let n = sel(s)
        if (!n) return notFound(s)
        let c = sel(containerSel)
        if (!c) return notFound(containerSel, "container ")
        let nr = n.getBoundingClientRect()
        if (nr.width <= 0 || nr.height <= 0) return { success: false }
        let cr = c.getBoundingClientRect()
        return rRes({ top: nr.top - cr.top, left: nr.left - cr.left })
    }
    // boxOf normalizes an element into the {top,...,centerX,centerY,clientWidth,clientHeight} shape Go's
    // newBox reads.  width/height/bottom/right come from getBoundingClientRect (border-box, includes the
    // scrollbar gutter); clientWidth/clientHeight are the scrollbar-excluded client box.
    let boxOf = (el) => { let x = el.getBoundingClientRect(); return { top: x.top, left: x.left, width: x.width, height: x.height, bottom: x.bottom, right: x.right, centerX: x.left + x.width / 2, centerY: x.top + x.height / 2, clientWidth: el.clientWidth, clientHeight: el.clientHeight } }
    // relativeBoxesP backs the pairwise geometry matchers (BeAbove/BeBelow/BeLeftOf/BeRightOf/Encloses/
    // Overlaps) and GetGapBetween/HaveGapBetween: poll until BOTH elements are present and laid out
    // (non-degenerate boxes), then read both viewport rectangles in a SINGLE eval so the relation is
    // judged at one layout instant.  Splitting into two BoundingBox reads loses that atomicity - a
    // mid-layout frame could satisfy neither-yet-both.  otherSel arrives already-encoded (Go encodes it).
    // Either endpoint being absent is an error that names WHICH selector went missing; a present-but-
    // degenerate box on either side stays a silent retry.
    b.relativeBoxesP = (s, otherSel) => {
        let n = sel(s)
        if (!n) return notFound(s)
        let o = sel(otherSel)
        if (!o) return notFound(otherSel, "other ")
        let nr = n.getBoundingClientRect(), or = o.getBoundingClientRect()
        if (nr.width <= 0 || nr.height <= 0 || or.width <= 0 || or.height <= 0) return { success: false }
        return rRes({ a: boxOf(n), b: boxOf(o) })
    }
    // inViewportP backs BeInViewport: once the element is present and laid out, report its rect alongside
    // the layout viewport size so Go can test on-screen-ness (does the box intersect the visible window).
    // Distinct from isVisible, which only checks the element is rendered at all - an element can be laid
    // out yet scrolled entirely out of view.
    b.inViewportP = one(n => {
        let x = n.getBoundingClientRect()
        if (x.width <= 0 || x.height <= 0) return { success: false }
        return rRes({ top: x.top, left: x.left, bottom: x.bottom, right: x.right, vw: window.innerWidth, vh: window.innerHeight })
    })
    // documentOrderP backs BePrecededBy/BeFollowedBy: once BOTH elements are present, return
    // compareDocumentPosition of other relative to the element so Go can test precedes/follows in
    // document order.  No layout gating - document order is structural, not geometric.  otherSel arrives
    // already-encoded.  Either endpoint being absent is an error that names WHICH selector went missing.
    b.documentOrderP = (s, otherSel) => {
        let n = sel(s)
        if (!n) return notFound(s)
        let o = sel(otherSel)
        if (!o) return notFound(otherSel, "other ")
        return rRes(n.compareDocumentPosition(o))
    }

    b.outline = () => {
        const PRUNE_TAGS = new Set(["script", "style", "svg"])
        const SELF_CLOSING = new Set(["area","base","br","col","embed","hr","img","input","link","meta","param","source","track","wbr"])
        const serializeAttrs = (el) => {
            let out = ""
            for (const a of el.attributes) out += ` ${a.name}="${a.value.replace(/"/g, "&quot;")}"`
            return out
        }
        const walk = (node, depth) => {
            const indent = "  ".repeat(depth)
            if (node.nodeType === Node.TEXT_NODE) {
                const t = node.textContent.replace(/\s+/g, " ").trim()
                return t ? indent + t + "\n" : ""
            }
            if (node.nodeType !== Node.ELEMENT_NODE) return ""
            const tag = node.tagName.toLowerCase()
            const open = indent + "<" + tag + serializeAttrs(node) + ">"
            if (SELF_CLOSING.has(tag)) return open + "\n"
            if (PRUNE_TAGS.has(tag)) return open + "…</" + tag + ">\n"
            let children = ""
            for (const child of node.childNodes) children += walk(child, depth + 1)
            return open + "\n" + children + indent + "</" + tag + ">\n"
        }
        let out = ""
        for (const child of document.body.childNodes) out += walk(child, 0)
        return rRes(out)
    }

    window["_biloba"] = b
}