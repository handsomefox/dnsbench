// The dashboard for `dnsbench -ui`. The server renders the form with its
// defaults. This script sends runs to /api/run and follows them over the
// /api/events stream. Event payloads are built in reporter.go. Keep the
// field names here in step with it.
"use strict"

const LOG_LIMIT = 80

const $ = (id) => document.getElementById(id)
const builtins = JSON.parse($("builtins").textContent)

const state = {
	runId: null,
	pending: false, // a POST /api/run is in flight
	status: "idle",
	totalResolvers: 0,
	active: null,
	results: new Map(), // key -> {server, stats, done}
	log: [],
}

// el builds an element. Text goes through textContent, so resolver names
// and error strings from the server are never parsed as HTML.
function el(tag, props = {}, ...children) {
	const node = document.createElement(tag)
	Object.assign(node, props)
	for (const child of children) {
		node.append(child)
	}
	return node
}

function serverKey(server) {
	return server.addr
}

function parseLines(text) {
	return text
		.split("\n")
		.map((line) => line.trim())
		.filter((line) => line && !line.startsWith("#"))
}

// parseResolvers reads "name;ip" lines. A line with no ";" is an address.
// The server validates every address and names unnamed resolvers.
function parseResolvers(text) {
	return parseLines(text).map((line) => {
		const parts = line.split(";").map((p) => p.trim())
		return parts.length === 1 ? { name: "", addr: parts[0] } : { name: parts[0], addr: parts[1] ?? "" }
	})
}

function useCustomResolvers() {
	return document.querySelector('input[name="resolver-source"]:checked').value === "custom"
}

// builtinSelection picks the built-in list for the current filters. The
// key must match builtinsKey in server.go.
function builtinSelection() {
	return builtins[`${$("only-major").checked}/${$("family").value}`] ?? []
}

function formatMs(value) {
	return typeof value === "number" && Number.isFinite(value) ? `${value.toFixed(1)} ms` : "—"
}

function successRate(stats) {
	return stats.total ? (stats.count / stats.total) * 100 : 0
}

// Config panel

function renderConfig() {
	const custom = useCustomResolvers()
	$("builtin-options").hidden = custom
	$("custom-options").hidden = !custom
	$("domain-count").textContent = parseLines($("domains").value).length

	const selection = builtinSelection()
	$("builtin-list").replaceChildren(
		...selection.map((s) => el("li", { title: s.addr }, el("strong", { textContent: s.name }), " ", s.addr)),
	)
	$("resolver-count").textContent = custom
		? parseResolvers($("custom-resolvers").value).length
		: selection.length
}

// Live and results panels. Query events can arrive thousands of times a
// second, so renders are batched to one per animation frame.

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
	$("start").disabled = running
	$("stop").disabled = !running

	const done = [...state.results.values()].filter((r) => r.done).length
	const pct = state.totalResolvers ? Math.round((done / state.totalResolvers) * 100) : 0
	$("progress-label").textContent = `${done}/${state.totalResolvers} resolvers`
	$("progress-pct").textContent = `${pct}%`
	$("progress").value = pct
	$("active").textContent = state.active ?? "nothing"

	$("log").replaceChildren(
		...state.log.map((entry) =>
			el(
				"li",
				{ className: entry.error ? "failed" : "" },
				el("span", {}, el("strong", { textContent: entry.server }), " ", entry.domain),
				el("span", { className: "value", title: entry.error ?? "", textContent: entry.error ?? formatMs(entry.latency) }),
			),
		),
	)

	renderResults()
}

function compareResults(sortBy) {
	// Resolvers with no successful lookup have a null mean. They sort last.
	const mean = (r) => r.stats.mean ?? Infinity
	if (sortBy === "mean") {
		return (a, b) => mean(a) - mean(b)
	}
	// The CLI's order: success rate first, then mean latency.
	return (a, b) => successRate(b.stats) - successRate(a.stats) || mean(a) - mean(b)
}

function renderResults() {
	const rows = [...state.results.values()].sort(compareResults($("sort").value))
	if (rows.length === 0) {
		$("results").replaceChildren(el("tr", {}, el("td", { colSpan: 4, className: "muted", textContent: "No results yet." })))
		return
	}
	$("results").replaceChildren(
		...rows.map(({ server, stats }) => {
			const rate = successRate(stats)
			return el(
				"tr",
				{},
				el("td", {}, el("strong", { textContent: server.name }), el("div", { className: "muted small", textContent: server.addr })),
				el("td", { className: "num" }, el("meter", { min: 0, max: 100, low: 90, high: 99, optimum: 100, value: rate }), ` ${rate.toFixed(1)}%`),
				el("td", { className: "num", textContent: formatMs(stats.mean) }),
				el("td", { className: "num small", textContent: `${formatMs(stats.min)} / ${formatMs(stats.max)}` }),
			)
		}),
	)
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

function emptyStats() {
	return { min: null, max: null, mean: null, count: 0, errors: 0, total: 0 }
}

function clearRun() {
	state.active = null
	state.totalResolvers = 0
	state.results = new Map()
	state.log = []
}

function handleEvent(msg) {
	const detail = msg.detail ?? {}
	switch (msg.type) {
		case "start":
			clearRun()
			state.status = "running"
			state.totalResolvers = detail.totalResolvers ?? 0
			break
		case "resolver_start":
			state.active = detail.server?.name ?? null
			break
		case "query": {
			const server = detail.server
			if (!server) break
			const failed = typeof detail.error === "string"
			state.log.unshift({ server: server.name, domain: detail.domain, latency: detail.latency, error: failed ? detail.error : null })
			state.log.length = Math.min(state.log.length, LOG_LIMIT)
			const key = serverKey(server)
			const entry = state.results.get(key) ?? { server, stats: emptyStats(), done: false }
			state.results.set(key, { ...entry, stats: liveStats(entry.stats, detail.latency, failed) })
			break
		}
		case "resolver_done":
			if (detail.server && detail.stats) {
				state.results.set(serverKey(detail.server), { server: detail.server, stats: detail.stats, done: true })
			}
			break
		case "complete":
			state.active = null
			for (const r of detail.results ?? []) {
				state.results.set(serverKey(r.server), { ...r, done: true })
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
	$("config").reset()
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
	const custom = useCustomResolvers()
	const resolvers = custom ? parseResolvers($("custom-resolvers").value) : []
	if (custom && resolvers.length === 0) {
		showNotice("Add at least one custom resolver, or switch to the built-in list.")
		return
	}
	const options = {
		repeats: Number($("repeats").value),
		timeoutMs: Number($("timeout").value),
		concurrency: Number($("concurrency").value),
		warmup: Number($("warmup").value),
		onlyMajor: $("only-major").checked,
		family: $("family").value,
	}
	// The new run's start event can arrive before the response. Clear the
	// old run first, and let acceptEvent adopt the new run ID from either.
	clearRun()
	state.runId = null
	state.pending = true
	showNotice("")
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
	events.onerror = () => showNotice("Lost the connection to the server. Retrying…")
}

$("config").addEventListener("submit", startRun)
$("config").addEventListener("input", renderConfig)
$("stop").addEventListener("click", stopRun)
$("reset").addEventListener("click", resetRun)
$("sort").addEventListener("change", renderResults)

renderConfig()
render()
connectEvents()
