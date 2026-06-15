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
    let one = (...chain) => (s, ...args) => {
        let n = sel(s)
        let errAnnotation = (typeof s == "string" ? ": " + s.slice(1) : "")
        if (!n) return rErr("could not find DOM element matching selector" + errAnnotation)
        for (let i = 0; i < chain.length - 1; i++) {
            let r = chain[i](n, ...args)
            if (!r.success) return !!r.error ? r : rErr(r.guard + errAnnotation)
        }
        let result = chain[chain.length - 1](n, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return result
    }
    let each = (cb) => (s, ...args) => {
        let ns = selEach(s)
        let errAnnotation = (typeof s == "string" ? ": " + s.slice(1) : "")

        let result = cb(ns, ...args)
        if (!!result.error) result.error = result.error + errAnnotation
        return result
    }
    let dispatchInputChange = (n) => {
        n.dispatchEvent(new Event('input', { bubbles: true }))
        n.dispatchEvent(new Event('change', { bubbles: true }))
    }
    b.exists = s => r(!!sel(s))
    b.count = each(ns => rRes(ns.length))
    b.isVisible = one(n => r(n.offsetWidth > 0 || n.offsetHeight > 0 || n.offsetParent != null, "DOM element is not visible"))
    b.isEnabled = one(n => r(!n.disabled, "DOM element is not enabled"))
    b.eachIsVisible = each(ns => r(ns.every(n => b.isVisible(n).success), "not all DOM elements are visible"))
    b.eachIsEnabled = each(ns => r(ns.every(n => b.isEnabled(n).success), "not all DOM elements are enabled"))
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
    b.click = one(b.isVisible, b.isEnabled, (n, o) => {
        o = o || {}
        if (plainPointer(o)) { n.click(); return r() }
        dispatchMouse(n, ['mousedown', 'mouseup', 'click'], { ...pointerOpts(n, o), button: 0, buttons: 1 })
        return r()
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
    b.scrollIntoView = one(n => r(n.scrollIntoView()))
    // isClickable is a deterministic, atomic occlusion/hittability check: visible + enabled +
    // the element (or a descendant) is the topmost thing at its own center point. elementFromPoint
    // is synchronous, so this stays in one JS snippet - no async round-trips, no new flakiness.
    // It fails fast (does not wait for animations); that is the deliberate stability tradeoff.
    b.isClickable = one(b.isVisible, b.isEnabled, n => {
        let rect = n.getBoundingClientRect()
        let cx = rect.left + rect.width / 2, cy = rect.top + rect.height / 2
        if (cx < 0 || cy < 0 || cx > window.innerWidth || cy > window.innerHeight) return r(false, "DOM element's center is outside the viewport (it would need to be scrolled into view)")
        let top = document.elementFromPoint(cx, cy)
        if (!top) return r(false, "DOM element is not hittable at its center point")
        return r(n === top || n.contains(top), "DOM element is obscured by another element")
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
        let top = inLocalViewport ? doc.elementFromPoint(lx, ly) : null
        let hittable = !!top && (n === top || n.contains(top))
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
    // boundingBox reports the first matching element's clip rectangle for page.CaptureScreenshot, in
    // CSS pixels relative to the TOP-LEVEL document (so x/y already include page scroll).  Like
    // measurePoint it walks the frameElement chain so an element inside a same-origin iframe is
    // translated to top-level page coordinates; the final +scrollX/+scrollY converts the top-level
    // viewport rect into document coordinates.  Errors on a zero-area element.
    b.boundingBox = one(n => {
        let rect = n.getBoundingClientRect()
        if (rect.width <= 0 || rect.height <= 0) return rErr("DOM element has zero area")
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
        return rRes({ x: left + top0.scrollX, y: top + top0.scrollY, width: rect.width, height: rect.height })
    })
    // scrollToStablePoint backs single-element realistic interactions: it scrolls the element to the
    // viewport center, waits for its box to stop moving (two consecutive animation frames with the
    // same rect - bounded so a perpetually-animating element can't hang), then returns measurePoint.
    // Async (returns a Promise); invoked with awaitPromise on the Go side.
    b.scrollToStablePoint = (s) => {
        let ann = (typeof s == "string" ? ": " + s.slice(1) : "")
        let n = sel(s)
        if (!n) return Promise.resolve(rErr("could not find DOM element matching selector" + ann))
        if (!b.isVisible(n).success) return Promise.resolve(rErr("DOM element is not visible" + ann))
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
        let ann = (typeof s == "string" ? ": " + s.slice(1) : "")
        let n = sel(s)
        if (!n) return Promise.resolve(rErr("could not find DOM element matching selector" + ann))
        if (!b.isVisible(n).success) return Promise.resolve(rErr("DOM element is not visible" + ann))
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
    b.getValue = one(n => {
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
    b.setValue = one(b.isVisible, b.isEnabled, (n, v) => {
        if (n.type == "select-one" && !n.querySelector(`[value="${v}"]`)) {
            return rErr(`Select input does not have option with value "${v}"`)
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
                let o = options.find(o => o.value == value)
                if (!o) return rErr(`The "${value}" option does not exist`)
                if (!b.isEnabled(o).success) return rErr(`The "${value}" option is not enabled`)
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
        return r()
    })
    b.getAttribute = one((n, a) => rRes(n.getAttribute(a)))
    b.hasAttribute = one((n, a) => r(n.hasAttribute(a)))
    b.isFocused = one(n => r(n === document.activeElement, "DOM element is not focused"))
    b.getComputedStyle = one((n, p) => rRes(window.getComputedStyle(n)[p]))
    b.hasProperty = one((n, p) => {
        let v = n
        for (const subP of p.split(".")) {
            if (!(subP in v)) return r(false)
            v = v[subP]
        }
        return r(true)
    })
    b.eachHasProperty = each((ns, p) => ns.length == 0 ? r(false) : r(ns.every(n => b.hasProperty(n, p).success)))
    b.getProperty = one((n, p) => {
        let v = n
        for (const subP of p.split(".")) {
            if (!(subP in v)) return rRes(null)
            v = v[subP]
        }
        if (v !== null && v !== undefined && !Array.isArray(v) && (typeof v == "object") && (typeof v[Symbol.iterator] == "function")) {
            v = Array.from(v)
        } else if (v instanceof DOMStringMap) {
            v = { ...v }
        }
        return rRes(v)
    })
    b.getPropertyForEach = each((ns, p) => rRes(ns.map(n => b.getProperty(n, p).result)))
    b.getProperties = one((n, ps) => rRes(ps.reduce((m, p) => {
        m[p] = b.getProperty(n, p).result
        return m
    }, {})))
    b.getPropertiesForEach = each((ns, ps) => rRes(ns.map(n => b.getProperties(n, ps).result)))
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
    b.invokeOn = one((n, f, ...args) => {
        if (!(f in n) || (typeof n[f] != "function")) return rErr(`element does not implement "${f}"`)
        return rRes(n[f](...args))
    })
    b.invokeOnEach = each((ns, f, ...args) => rRes(ns.map(n => b.invokeOn(n, f, ...args).result)))
    b.invokeWith = one((n, script, ...args) => rRes(eval(script)(n, ...args)))
    b.invokeWithEach = each((ns, script, ...args) => rRes(ns.map(n => b.invokeWith(n, script, ...args).result)))

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