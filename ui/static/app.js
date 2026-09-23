// The dashboard for `dnsbench -ui`. The server renders the form with its
// defaults. This script sends runs to /api/run and follows them over the
// /api/events stream. Event payloads are built in reporter.go. Keep the
// field names here in step with it.
"use strict"

const LOG_LIMIT = 100
const AXIS_MIN_MS = 1
const FAIL_GUTTER = 30 // px at the right of each trace for failed lookups

const $ = (id) => document.getElementById(id)
const form = $("config")
const builtins = JSON.parse($("builtins").textContent)
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)")

const state = {
	runId: null,
	pending: false, // a POST /api/run is in flight
	status: "idle",
	totalResolvers: 0,
	doneResolvers: 0,
	plannedLookups: 0,
	lookups: 0,
	active: null,
	axisMaxMs: 3000,
	results: new Map(), // serverKey -> entry, see entryFor
	log: [],
	expanded: new Set(),
}

// Built-in resolvers the user unchecked, by serverKey.
const excluded = new Set()

// el builds an element. Text goes through textContent, so resolver names
// and error strings from the server are never parsed as HTML.
function el(tag, props = {}, ...children) {
	const node = document.createElement(tag)
	Object.assign(node, props)
	for (const child of children) {
		if (child !== null && child !== undefined) node.append(child)
	}
	return node
}

function transportOf(server) {
	if (server.doqName) return "doq"
	if (server.dohURL) return "doh"
	if (server.tlsName) return "dot"
	return "plain"
}

// serverKey tells results apart. Every transport can share an address.
function serverKey(server) {
	return `${transportOf(server)} ${server.addr}`
}

function serverTags(server) {
	const tags = [server.addr.includes(":") ? "IPv6" : "IPv4"]
	if (server.tlsName) tags.push("DoT")
	if (server.dohURL) tags.push("DoH")
	if (server.doqName) tags.push("DoQ")
	return tags
}

function transportLabel(server) {
	if (server.doqName) return `DoQ with certificate name ${server.doqName}`
	if (server.dohURL) return `DoH at ${server.dohURL}`
	if (server.tlsName) return `DoT with certificate name ${server.tlsName}`
	return "plain DNS"
}

function formatMs(value) {
	if (typeof value !== "number" || !Number.isFinite(value)) return "—"
	return value < 10 ? `${value.toFixed(1)} ms` : `${Math.round(value)} ms`
}

function formatPct(value) {
	return `${value.toFixed(value === 100 ? 0 : 1)}%`
}

function successRate(stats) {
	return stats.total ? (stats.count / stats.total) * 100 : 0
}

function percentile(sorted, p) {
	if (sorted.length === 0) return null
	const i = (sorted.length - 1) * p
	const lo = Math.floor(i)
	const hi = Math.ceil(i)
	return sorted[lo] + (sorted[hi] - sorted[lo]) * (i - lo)
}

// errorKind shortens an error string to the part that tells failures apart.
function errorKind(message) {
	if (/certificate/i.test(message)) return "bad TLS certificate"
	if (/no route/i.test(message)) return "no route to resolver"
	if (/timeout|deadline/i.test(message)) return "timeout"
	if (/no such host/i.test(message)) return "no such host"
	if (/refused/i.test(message)) return "connection refused"
	if (/reset by peer/i.test(message)) return "connection reset"
	const parts = message.split(": ")
	return parts[parts.length - 1]
}

function parseLines(text) {
	return text
		.split("\n")
		.map((line) => line.trim())
		.filter((line) => line && !line.startsWith("#"))
}

// parseResolvers reads the lines of a -f file: "name;ip", "name;ip;tls-name",
// "name;ip;https-url", or "name;ip;quic://tls-name". A line with no ";" is an address. The server
// validates every field and names unnamed resolvers.
function parseResolvers(text) {
	return parseLines(text).map((line) => {
		const parts = line.split(";").map((p) => p.trim())
		if (parts.length === 1) return { name: "", addr: parts[0] }
		const server = { name: parts[0], addr: parts[1] }
		if (parts[2]?.startsWith("https://")) server.dohURL = parts[2]
		else if (parts[2]?.startsWith("quic://")) server.doqName = parts[2].slice("quic://".length)
		else if (parts[2]) server.tlsName = parts[2]
		return server
	})
}

function useCustomResolvers() {
	return form.elements["resolver-source"].value === "custom"
}

// builtinSelection picks the built-in list for the current filters. The
// key must match builtinsKey in server.go.
function builtinSelection() {
	const key = `${$("only-major").checked}/${$("primary-only").checked}/${form.elements.family.value}/${form.elements.transport.value}`
	return builtins[key] ?? []
}

function selectedBuiltins() {
	return builtinSelection().filter((s) => !excluded.has(serverKey(s)))
}

// Setup panel

function renderConfig() {
	const custom = useCustomResolvers()
	$("builtin-options").hidden = custom
	$("custom-options").hidden = !custom
	$("domain-count").textContent = parseLines($("domains").value).length

	if (custom) {
		const n = parseResolvers($("custom-resolvers").value).length
		$("resolver-count").textContent = `${n} ${n === 1 ? "resolver" : "resolvers"}`
		return
	}

	const selection = builtinSelection()
	const chosen = selection.filter((s) => !excluded.has(serverKey(s))).length
	$("resolver-count").textContent = `${chosen} of ${selection.length} selected`
	$("builtin-list").replaceChildren(
		...selection.map((s) => {
			const key = serverKey(s)
			const box = el("input", { type: "checkbox", checked: !excluded.has(key) })
			box.dataset.key = key
			return el(
				"li",
				{},
				el(
					"label",
					{ title: `${s.name}, ${s.addr}, ${transportLabel(s)}` },
					box,
					el("span", { className: "name", textContent: s.name }),
					el("span", { className: "addr", textContent: s.addr }),
				),
			)
		}),
	)
}

// Board. Query events can arrive thousands of times a second, so renders
// are batched to one per animation frame, and only changed traces redraw.

let renderQueued = false

function queueRender() {
	if (renderQueued) return
	renderQueued = true
	requestAnimationFrame(() => {
		renderQueued = false
		render()
	})
}

function render() {
	const running = state.status === "running"
	$("status").textContent = state.status
	$("status").dataset.status = state.status
	$("start").disabled = running || state.pending
	$("stop").disabled = !running
	$("setup-fields").disabled = running || state.pending

	renderProgress()
	renderSummary()
	renderLadder()
	renderLog()
}

function renderProgress() {
	let pct = 0
	if (state.plannedLookups) {
		pct = Math.min(100, (state.lookups / state.plannedLookups) * 100)
	} else if (state.totalResolvers) {
		pct = (state.doneResolvers / state.totalResolvers) * 100
	}
	if (state.status === "complete") pct = 100
	$("progress-fill").style.width = `${pct}%`

	let line = "Pick resolvers and domains, then start a benchmark."
	if (state.status === "running" && state.active) {
		line = `Resolver ${Math.min(state.doneResolvers + 1, state.totalResolvers)} of ${state.totalResolvers}: ${state.active}`
	} else if (state.status === "running") {
		line = "Starting…"
	} else if (state.status === "complete") {
		line = `Finished ${state.totalResolvers} resolvers and ${state.lookups.toLocaleString()} lookups.`
	} else if (state.status === "stopped") {
		line = `Stopped after ${state.doneResolvers} of ${state.totalResolvers} resolvers.`
	} else if (state.status === "error") {
		line = "The run ended with an error."
	}
	$("run-line").textContent = line
}

function renderSummary() {
	const entries = [...state.results.values()]
	if (entries.length === 0) {
		$("summary").textContent = ";; no results yet"
		return
	}
	const answered = entries.filter((e) => e.samples.length > 0)
	const fastest = answered.reduce((best, e) => (best === null || median(e) < median(best) ? e : best), null)
	const reliable = entries
		.filter((e) => e.done)
		.reduce((best, e) => {
			if (best === null) return e
			const d = successRate(e.stats) - successRate(best.stats)
			return d > 0 || (d === 0 && (median(e) ?? Infinity) < (median(best) ?? Infinity)) ? e : best
		}, null)
	const failed = entries.filter((e) => e.done && e.samples.length === 0 && e.stats.count === 0).length

	const lookups = state.plannedLookups
		? `${state.lookups.toLocaleString()} of ${state.plannedLookups.toLocaleString()}`
		: state.lookups.toLocaleString()
	const rows = [
		["fastest median", fastest ? `${fastest.server.name}  ${formatMs(median(fastest))}` : "—"],
		["most reliable", reliable ? `${reliable.server.name}  ${formatPct(successRate(reliable.stats))}` : "—"],
		["lookups", lookups],
		["no answer at all", failed ? `${failed} ${failed === 1 ? "resolver" : "resolvers"}` : "none"],
	]
	$("summary").textContent = rows.map(([k, v]) => `;; ${k.padEnd(17)} ${v}`).join("\n")
}

// Entries

function entryFor(server) {
	const key = serverKey(server)
	let entry = state.results.get(key)
	if (!entry) {
		entry = {
			key,
			server,
			stats: emptyStats(),
			done: false,
			samples: [], // latency of each successful lookup, in ms
			sorted: null, // samples, sorted, rebuilt on demand
			errors: new Map(), // errorKind -> count
			domains: new Map(), // domain -> latencies
			dirty: true,
		}
		state.results.set(key, entry)
	}
	return entry
}

function sortedSamples(entry) {
	if (!entry.sorted) entry.sorted = [...entry.samples].sort((a, b) => a - b)
	return entry.sorted
}

function median(entry) {
	return percentile(sortedSamples(entry), 0.5)
}

function p95(entry) {
	return percentile(sortedSamples(entry), 0.95)
}

function emptyStats() {
	return { min: null, max: null, mean: null, count: 0, errors: 0, total: 0 }
}

// liveStats folds one lookup into a resolver's running statistics until
// the resolver_done event replaces them with the server's numbers.
function liveStats(prev, latency, failed) {
	const next = { ...prev, total: prev.total + 1 }
	if (failed) {
		next.errors += 1
		return next
	}
	next.count += 1
	next.mean = (prev.mean ?? 0) + (latency - (prev.mean ?? 0)) / next.count
	next.min = prev.min === null ? latency : Math.min(prev.min, latency)
	next.max = prev.max === null ? latency : Math.max(prev.max, latency)
	return next
}

// Ladder

const rowNodes = new Map() // serverKey -> {li, canvas, p50, p95, ok, detail}
let rowOrder = []
let palette = null

function readPalette() {
	const css = getComputedStyle(document.documentElement)
	const v = (name) => css.getPropertyValue(name).trim()
	palette = { dot: v("--dot"), ink: v("--ink"), rule: v("--rule"), fail: v("--fail"), failBg: v("--fail-bg") }
}

function axisX(ms, width) {
	const lo = Math.log10(AXIS_MIN_MS)
	const hi = Math.log10(state.axisMaxMs)
	const t = (Math.log10(Math.max(ms, AXIS_MIN_MS)) - lo) / (hi - lo)
	return Math.min(1, Math.max(0, t)) * width
}

function axisTicks() {
	return [1, 3, 10, 30, 100, 300, 1000, 3000, 10000, 30000].filter((t) => t <= state.axisMaxMs)
}

function renderAxis() {
	$("axis").replaceChildren(
		...axisTicks().map((t) => {
			const x = axisX(t, 1)
			const span = el("span", { textContent: t >= 1000 ? `${t / 1000}s` : `${t}` })
			span.style.left = `calc((100% - ${FAIL_GUTTER}px) * ${x})`
			// Keep the labels at either end inside the axis.
			if (x < 0.03) span.className = "start"
			if (x > 0.97) span.className = "end"
			return span
		}),
		el("span", { className: "gutter-label", textContent: "fail" }),
	)
}

function compareEntries(sortBy) {
	const orInf = (v) => v ?? Infinity
	switch (sortBy) {
		case "name":
			return (a, b) => a.server.name.localeCompare(b.server.name)
		case "success":
			return (a, b) => successRate(b.stats) - successRate(a.stats) || orInf(median(a)) - orInf(median(b))
		case "p95":
			return (a, b) => orInf(p95(a)) - orInf(p95(b))
		default:
			return (a, b) => orInf(median(a)) - orInf(median(b))
	}
}

function makeRow(entry) {
	const canvas = el("canvas", { className: "trace" })
	const head = el(
		"button",
		{ type: "button", className: "row-head" },
		el("span", { className: "name", textContent: entry.server.name }),
		el(
			"span",
			{ className: "meta" },
			el("span", { className: "addr", textContent: entry.server.addr }),
			...serverTags(entry.server).map((t) => el("span", { className: `tag tag-${t.toLowerCase()}`, textContent: t })),
		),
	)
	head.setAttribute("aria-expanded", "false")
	const nodes = {
		li: el("li", { className: "row" }),
		head,
		canvas,
		p50: el("span", { className: "num" }),
		p95: el("span", { className: "num" }),
		ok: el("span", { className: "num" }),
		detail: el("div", { className: "detail", hidden: true }),
	}
	nodes.li.append(el("div", { className: "grid-row" }, head, canvas, nodes.p50, nodes.p95, nodes.ok), nodes.detail)
	head.addEventListener("click", () => {
		if (state.expanded.has(entry.key)) state.expanded.delete(entry.key)
		else state.expanded.add(entry.key)
		entry.dirty = true
		queueRender()
	})
	if (!reducedMotion.matches) nodes.li.classList.add("arrive")
	return nodes
}

function renderLadder() {
	const entries = [...state.results.values()]
	$("ladder-empty").hidden = entries.length > 0
	document.body.classList.toggle("has-results", entries.length > 0)
	$("export-csv").disabled = entries.length === 0
	$("export-json").disabled = entries.length === 0

	const ordered = entries.sort(compareEntries($("sort").value))
	const order = ordered.map((e) => e.key)
	for (const entry of ordered) {
		if (!rowNodes.has(entry.key)) rowNodes.set(entry.key, makeRow(entry))
	}
	if (order.join("|") !== rowOrder.join("|")) {
		$("ladder").replaceChildren(...order.map((k) => rowNodes.get(k).li))
		rowOrder = order
	}

	for (const entry of ordered) {
		if (!entry.dirty) continue
		entry.dirty = false
		const nodes = rowNodes.get(entry.key)
		const rate = successRate(entry.stats)
		nodes.p50.textContent = formatMs(median(entry))
		nodes.p95.textContent = formatMs(p95(entry))
		nodes.ok.textContent = entry.stats.total ? formatPct(rate) : "—"
		nodes.ok.classList.toggle("bad", entry.stats.total > 0 && rate < 100)
		nodes.li.classList.toggle("pending", !entry.done)
		drawTrace(entry, nodes.canvas)

		const open = state.expanded.has(entry.key)
		nodes.head.setAttribute("aria-expanded", String(open))
		nodes.detail.hidden = !open
		if (open) renderDetail(entry, nodes.detail)
	}
}

function drawTrace(entry, canvas) {
	const dpr = window.devicePixelRatio || 1
	const width = canvas.clientWidth
	const height = canvas.clientHeight
	if (width === 0) return
	if (canvas.width !== Math.round(width * dpr) || canvas.height !== Math.round(height * dpr)) {
		canvas.width = Math.round(width * dpr)
		canvas.height = Math.round(height * dpr)
	}
	const ctx = canvas.getContext("2d")
	ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
	ctx.clearRect(0, 0, width, height)

	const plot = width - FAIL_GUTTER
	const mid = height / 2

	// Decade gridlines, like log paper.
	ctx.fillStyle = palette.rule
	for (const t of axisTicks()) {
		ctx.fillRect(Math.round(axisX(t, plot)), 2, 1, height - 4)
	}

	// One dot per successful lookup, spread vertically by a fixed hash so a
	// dense cluster reads as dense instead of as one dot.
	ctx.fillStyle = palette.dot
	ctx.globalAlpha = 0.5
	entry.samples.forEach((ms, i) => {
		const jitter = (((i * 2654435761) >>> 0) % 1000) / 1000 - 0.5
		ctx.beginPath()
		ctx.arc(axisX(ms, plot), mid + jitter * (height - 12), 1.8, 0, Math.PI * 2)
		ctx.fill()
	})
	ctx.globalAlpha = 1

	const p50v = median(entry)
	if (p50v !== null) {
		ctx.fillStyle = palette.ink
		ctx.fillRect(Math.round(axisX(p50v, plot)) - 1, 3, 2, height - 6)
		const x95 = axisX(p95(entry), plot)
		ctx.beginPath()
		ctx.moveTo(x95 - 4, 2)
		ctx.lineTo(x95 + 4, 2)
		ctx.lineTo(x95, 8)
		ctx.closePath()
		ctx.fill()
	}

	// Failed lookups fill the gutter from the bottom, by share of lookups.
	const failures = entry.stats.errors
	ctx.fillStyle = palette.failBg
	ctx.fillRect(plot + 6, 3, FAIL_GUTTER - 8, height - 6)
	if (failures > 0 && entry.stats.total > 0) {
		const h = Math.max(2, (height - 6) * (failures / entry.stats.total))
		ctx.fillStyle = palette.fail
		ctx.fillRect(plot + 6, height - 3 - h, FAIL_GUTTER - 8, h)
	}

	canvas.title = entry.samples.length
		? `${entry.samples.length} answers, ${failures} failed. min ${formatMs(sortedSamples(entry)[0])}, p50 ${formatMs(p50v)}, p95 ${formatMs(p95(entry))}, max ${formatMs(sortedSamples(entry).at(-1))}`
		: `${failures} failed lookups`
}

function renderDetail(entry, node) {
	const s = entry.stats
	const lines = [
		`;; lookups ${s.total}   answered ${s.count}   failed ${s.errors}`,
		`;; min ${formatMs(s.min)}   mean ${formatMs(s.mean)}   max ${formatMs(s.max)}`,
	]
	if (transportOf(entry.server) !== "plain") lines.push(`;; ${transportLabel(entry.server)}`)

	const slow = [...entry.domains.entries()]
		.map(([domain, values]) => {
			const sorted = [...values].sort((a, b) => a - b)
			return { domain, p50: percentile(sorted, 0.5), n: values.length }
		})
		.sort((a, b) => b.p50 - a.p50)
		.slice(0, 5)

	const errors = [...entry.errors.entries()].sort((a, b) => b[1] - a[1])

	node.replaceChildren(
		el("pre", { textContent: lines.join("\n") }),
		el(
			"div",
			{ className: "detail-cols" },
			el(
				"div",
				{},
				el("h3", { textContent: "Slowest domains, by median" }),
				slow.length
					? el("ol", {}, ...slow.map((d) => el("li", {}, el("span", { textContent: d.domain }), el("span", { className: "num", textContent: formatMs(d.p50) }))))
					: el("p", { className: "hint", textContent: "No answers yet." }),
			),
			el(
				"div",
				{},
				el("h3", { textContent: "Errors" }),
				errors.length
					? el("ol", {}, ...errors.map(([kind, n]) => el("li", {}, el("span", { textContent: kind }), el("span", { className: "num", textContent: n.toLocaleString() }))))
					: el("p", { className: "hint", textContent: "None." }),
			),
		),
	)
}

function renderLog() {
	$("log-count").textContent = state.log.length ? `last ${state.log.length}` : ""
	if (!document.querySelector("details.log").open) return
	$("log").replaceChildren(
		...state.log.map((entry) =>
			el(
				"li",
				{ className: entry.error ? "failed" : "" },
				el("span", { className: "who", textContent: entry.server }),
				el("span", { className: "what", textContent: entry.domain }),
				el("span", { className: "value", title: entry.error ?? "", textContent: entry.error ? errorKind(entry.error) : formatMs(entry.latency) }),
			),
		),
	)
}

// Events

function clearRun() {
	state.active = null
	state.totalResolvers = 0
	state.doneResolvers = 0
	state.plannedLookups = 0
	state.lookups = 0
	state.results = new Map()
	state.log = []
	state.expanded = new Set()
	rowNodes.clear()
	rowOrder = []
	$("ladder").replaceChildren()
}

function handleEvent(msg) {
	const detail = msg.detail ?? {}
	switch (msg.type) {
		case "start": {
			const repeats = state.repeats
			clearRun()
			state.status = "running"
			state.totalResolvers = detail.totalResolvers ?? 0
			if (repeats && detail.domainCount) {
				state.plannedLookups = repeats * detail.domainCount * state.totalResolvers
			}
			renderAxis()
			break
		}
		case "resolver_start":
			state.active = detail.server?.name ?? null
			if (detail.server) entryFor(detail.server)
			break
		case "query": {
			const server = detail.server
			if (!server) break
			const failed = typeof detail.error === "string"
			state.lookups += 1
			state.log.unshift({ server: server.name, domain: detail.domain, latency: detail.latency, error: failed ? detail.error : null })
			state.log.length = Math.min(state.log.length, LOG_LIMIT)

			const entry = entryFor(server)
			entry.stats = liveStats(entry.stats, detail.latency, failed)
			if (failed) {
				const kind = errorKind(detail.error)
				entry.errors.set(kind, (entry.errors.get(kind) ?? 0) + 1)
			} else {
				entry.samples.push(detail.latency)
				entry.sorted = null
				const perDomain = entry.domains.get(detail.domain) ?? []
				perDomain.push(detail.latency)
				entry.domains.set(detail.domain, perDomain)
			}
			entry.dirty = true
			break
		}
		case "resolver_done":
			if (detail.server && detail.stats) {
				const entry = entryFor(detail.server)
				entry.stats = detail.stats
				if (!entry.done) state.doneResolvers += 1
				entry.done = true
				entry.dirty = true
			}
			break
		case "complete":
			state.active = null
			for (const r of detail.results ?? []) {
				const entry = entryFor(r.server)
				entry.stats = r.stats
				entry.done = true
				entry.dirty = true
			}
			// A stopped run still completes, with a "context canceled" error.
			if (state.status === "stopped") break
			state.status = detail.error ? "error" : "complete"
			if (detail.error) showNotice(detail.error)
			break
		case "stop":
			state.active = null
			state.status = "stopped"
			break
		case "reset":
			resetPage()
			break
		default:
			return
	}
	queueRender()
}

function showNotice(text) {
	$("notice").textContent = text
	$("notice").hidden = !text
}

function resetPage() {
	clearRun()
	state.runId = null
	state.status = "idle"
	form.reset()
	excluded.clear()
	showNotice("")
	renderConfig()
	queueRender()
}

// Actions

async function post(path, body) {
	const res = await fetch(path, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: body === undefined ? undefined : JSON.stringify(body),
	})
	if (!res.ok) {
		throw new Error((await res.text()).trim() || `${path} returned ${res.status}`)
	}
	return res.status === 204 ? null : res.json()
}

async function startRun(event) {
	event.preventDefault()
	const domains = parseLines($("domains").value)
	if (domains.length === 0) {
		showNotice("Add at least one domain.")
		return
	}

	let resolvers = []
	if (useCustomResolvers()) {
		resolvers = parseResolvers($("custom-resolvers").value)
		if (resolvers.length === 0) {
			showNotice("Add at least one resolver, or switch to the built-in list.")
			return
		}
	} else {
		const chosen = selectedBuiltins()
		if (chosen.length === 0) {
			showNotice("Select at least one resolver.")
			return
		}
		// With the whole list selected, send none and let the server pick
		// the same list from the options.
		if (chosen.length < builtinSelection().length) resolvers = chosen
	}

	const options = {
		repeats: Number($("repeats").value),
		timeoutMs: Number($("timeout").value),
		concurrency: Number($("concurrency").value),
		warmup: Number($("warmup").value),
		onlyMajor: $("only-major").checked,
		primaryOnly: $("primary-only").checked,
		family: form.elements.family.value,
		transport: form.elements.transport.value,
	}

	// The new run's start event can arrive before the response. Clear the
	// old run first, and let acceptEvent adopt the new run ID from either.
	clearRun()
	state.runId = null
	state.pending = true
	state.repeats = options.repeats
	state.axisMaxMs = Math.max(1000, options.timeoutMs)
	renderAxis()
	showNotice("")
	queueRender()
	try {
		const { runId } = await post("/api/run", { domains, resolvers, options })
		state.runId = runId
		state.status = "running"
	} catch (err) {
		showNotice(err.message)
	} finally {
		state.pending = false
		queueRender()
	}
}

async function stopRun() {
	try {
		await post("/api/stop")
	} catch (err) {
		showNotice(err.message)
	}
}

async function resetRun() {
	try {
		await post("/api/reset")
		resetPage()
	} catch (err) {
		showNotice(err.message)
	}
}

// Export

function exportRows() {
	return [...state.results.values()].sort(compareEntries($("sort").value)).map((e) => ({
		name: e.server.name,
		addr: e.server.addr,
		tlsName: e.server.tlsName ?? "",
		dohURL: e.server.dohURL ?? "",
		doqName: e.server.doqName ?? "",
		transport: transportOf(e.server),
		successPct: Number(successRate(e.stats).toFixed(2)),
		answered: e.stats.count,
		failed: e.stats.errors,
		total: e.stats.total,
		p50Ms: roundOrNull(median(e)),
		p95Ms: roundOrNull(p95(e)),
		meanMs: roundOrNull(e.stats.mean),
		minMs: roundOrNull(e.stats.min),
		maxMs: roundOrNull(e.stats.max),
	}))
}

function roundOrNull(v) {
	return typeof v === "number" && Number.isFinite(v) ? Number(v.toFixed(3)) : null
}

function download(filename, type, text) {
	const url = URL.createObjectURL(new Blob([text], { type }))
	const a = el("a", { href: url, download: filename })
	document.body.append(a)
	a.click()
	a.remove()
	URL.revokeObjectURL(url)
}

function exportName(ext) {
	return `dnsbench-${new Date().toISOString().slice(0, 19).replace(/:/g, "")}.${ext}`
}

function exportCSV() {
	const rows = exportRows()
	const cols = Object.keys(rows[0] ?? { name: "" })
	const cell = (v) => {
		const s = v === null ? "" : String(v)
		return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
	}
	const text = [cols.join(","), ...rows.map((r) => cols.map((c) => cell(r[c])).join(","))].join("\n")
	download(exportName("csv"), "text/csv", `${text}\n`)
}

function exportJSON() {
	download(exportName("json"), "application/json", `${JSON.stringify(exportRows(), null, 2)}\n`)
}

// acceptEvent drops events from runs other than the one on screen. Reset
// events carry no run ID and always pass.
function acceptEvent(msg) {
	if (!msg.runId) return true
	if (state.pending && !state.runId) {
		// Only the new run's start can arrive now. Anything else is the
		// tail of the run it replaced.
		if (msg.type !== "start") return false
		state.runId = msg.runId
		return true
	}
	// A page opened mid-run follows whatever run is going.
	if (!state.runId) state.runId = msg.runId
	return msg.runId === state.runId
}

function connectEvents() {
	const events = new EventSource("/api/events")
	events.onmessage = (event) => {
		const msg = JSON.parse(event.data)
		if (msg.type === "ready" || msg.type === "ping") return
		if (acceptEvent(msg)) handleEvent(msg)
	}
	events.onopen = () => showNotice("")
	events.onerror = () => showNotice("Lost the connection to dnsbench. Retrying…")
}

function markAllDirty() {
	for (const entry of state.results.values()) entry.dirty = true
	queueRender()
}

form.addEventListener("submit", startRun)
// A checkbox fires input before change, and renderConfig replaces the
// picker's checkboxes. So record the choice before any re-render, in one
// handler for both events.
function onSetupChange(event) {
	const key = event.target.dataset?.key
	if (key) {
		if (event.target.checked) excluded.delete(key)
		else excluded.add(key)
	}
	renderConfig()
}
form.addEventListener("input", onSetupChange)
form.addEventListener("change", onSetupChange)
$("timeout").addEventListener("change", () => {
	if (state.status !== "running") {
		state.axisMaxMs = Math.max(1000, Number($("timeout").value) || 3000)
		renderAxis()
		markAllDirty()
	}
})
$("select-all").addEventListener("click", () => {
	for (const s of builtinSelection()) excluded.delete(serverKey(s))
	renderConfig()
})
$("select-none").addEventListener("click", () => {
	for (const s of builtinSelection()) excluded.add(serverKey(s))
	renderConfig()
})
$("stop").addEventListener("click", stopRun)
$("reset").addEventListener("click", resetRun)
$("sort").addEventListener("change", queueRender)
$("export-csv").addEventListener("click", exportCSV)
$("export-json").addEventListener("click", exportJSON)
document.querySelector("details.log").addEventListener("toggle", queueRender)
window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
	readPalette()
	markAllDirty()
})
new ResizeObserver(markAllDirty).observe($("ladder"))

state.axisMaxMs = Math.max(1000, Number($("timeout").value) || 3000)
readPalette()
renderAxis()
renderConfig()
render()
connectEvents()
