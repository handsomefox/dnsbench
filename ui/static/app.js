// The dashboard for `dnsbench -ui`. The server renders the form with its
// defaults and embeds the built-in resolver catalog. This script picks
// resolvers from the catalog, sends runs to /api/run with an explicit
// resolver list, and follows them over the /api/events stream. Event
// payloads are built in reporter.go. Keep the field names here in step
// with it.
"use strict"

const LOG_LIMIT = 100
const AXIS_MIN_MS = 1
const FAIL_GUTTER = 30 // px at the right of each trace for failed lookups
const STORAGE_KEY = "dnsbench.setup.v1"

const $ = (id) => document.getElementById(id)
const form = $("config")
const catalog = JSON.parse($("builtins").textContent)
const defaultDomains = JSON.parse($("default-domains").textContent)
const serverDefaults = JSON.parse($("defaults").textContent)
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)")

const ALL = {
	family: ["ipv4", "ipv6"],
	transport: ["plain", "dot", "doh", "doq"],
	category: ["Global", "Filtering", "Privacy", "Regional"],
}

const QUICK_DOMAINS = [
	"google.com", "youtube.com", "facebook.com", "wikipedia.org", "amazon.com", "reddit.com",
	"github.com", "netflix.com", "microsoft.com", "cloudflare.com", "instagram.com", "linkedin.com",
].filter((d) => defaultDomains.includes(d))

const DOMAIN_SETS = { quick: QUICK_DOMAINS, all: defaultDomains }

const PRESETS = [
	{
		id: "quick",
		name: "Quick check",
		about: "Major providers over plain DNS, one address each, on 12 popular sites.",
		family: ["ipv4"], transport: ["plain"], category: ALL.category, major: true, primary: true, domains: "quick", repeats: 3,
	},
	{
		id: "providers",
		name: "Every provider",
		about: "Every provider over plain DNS, one address each.",
		family: ["ipv4"], transport: ["plain"], category: ALL.category, major: false, primary: true, domains: "all", repeats: 5,
	},
	{
		id: "encrypted",
		name: "Encrypted DNS",
		about: "DoT, DoH, and DoQ from the major providers.",
		family: ["ipv4"], transport: ["dot", "doh", "doq"], category: ALL.category, major: true, primary: true, domains: "all", repeats: 5,
	},
	{
		id: "versus",
		name: "Plain vs encrypted",
		about: "Every transport from the major providers, side by side.",
		family: ["ipv4"], transport: ALL.transport, category: ALL.category, major: true, primary: true, domains: "all", repeats: 5,
	},
	{
		id: "ipv6",
		name: "IPv4 vs IPv6",
		about: "Both address families of the major providers over plain DNS.",
		family: ALL.family, transport: ["plain"], category: ALL.category, major: true, primary: true, domains: "all", repeats: 5,
	},
	{
		id: "privacy",
		name: "Privacy resolvers",
		about: "No-logging operators over DoT and DoH.",
		family: ["ipv4"], transport: ["dot", "doh"], category: ["Privacy"], major: false, primary: true, domains: "all", repeats: 5,
	},
	{
		id: "everything",
		name: "Everything",
		about: "Every resolver, address, and transport. Takes a while.",
		family: ALL.family, transport: ALL.transport, category: ALL.category, major: false, primary: false, domains: "all", repeats: 3,
	},
]

const state = {
	runId: null,
	pending: false, // a POST /api/run is in flight
	status: "idle",
	startedAt: 0,
	totalResolvers: 0,
	doneResolvers: 0,
	plannedLookups: 0,
	lookups: 0,
	active: null,
	axisMaxMs: 3000,
	results: new Map(), // serverKey -> entry, see entryFor
	log: [],
	expanded: new Set(),
	hiddenTransports: new Set(), // result filter
}

// setup holds the resolver filters. The form holds the rest.
let setup = defaultSetup()

function defaultSetup() {
	const pick = (value, all) => (value === "all" || !value ? all : [value])
	return {
		family: new Set(pick(serverDefaults.family, ALL.family)),
		transport: new Set(pick(serverDefaults.transport, ALL.transport)),
		category: new Set(ALL.category),
		major: Boolean(serverDefaults.onlyMajor),
		primary: Boolean(serverDefaults.primaryOnly),
		excluded: new Set(), // serverKeys the user unchecked
		open: new Set(), // providers expanded in the picker
		search: "",
	}
}

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

const TRANSPORT_LABEL = { plain: "Plain", dot: "DoT", doh: "DoH", doq: "DoQ" }

function serverTags(server) {
	const tags = [server.addr.includes(":") ? "IPv6" : "IPv4"]
	const t = transportOf(server)
	if (t !== "plain") tags.push(TRANSPORT_LABEL[t])
	return tags
}

function transportLabel(server) {
	if (server.doqName) return `DoQ with certificate name ${server.doqName}`
	if (server.dohURL) return `DoH at ${server.dohURL}`
	if (server.tlsName) return `DoT with certificate name ${server.tlsName}`
	return "plain DNS"
}

// wireServer strips a catalog entry to the fields /api/run takes.
function wireServer(s) {
	const out = { name: s.name, addr: s.addr }
	if (s.tlsName) out.tlsName = s.tlsName
	if (s.dohURL) out.dohURL = s.dohURL
	if (s.doqName) out.doqName = s.doqName
	return out
}

function formatMs(value) {
	if (typeof value !== "number" || !Number.isFinite(value)) return "—"
	return value < 10 ? `${value.toFixed(1)} ms` : `${Math.round(value)} ms`
}

function formatPct(value) {
	return `${value.toFixed(value === 100 ? 0 : 1)}%`
}

function formatDuration(ms) {
	const s = Math.max(0, Math.round(ms / 1000))
	if (s < 60) return `${s} s`
	const m = Math.floor(s / 60)
	return s % 60 ? `${m} min ${s % 60} s` : `${m} min`
}

function plural(n, word) {
	return `${n.toLocaleString()} ${word}${n === 1 ? "" : "s"}`
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
	if (/refused the TLS handshake/i.test(message)) return "TLS handshake refused"
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

// Custom resolvers

const HOSTNAME = /^(?=.{1,253}$)([A-Za-z0-9_](?:[A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?\.)+[A-Za-z0-9_](?:[A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$/
const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/
const IPV6 = /^[0-9a-f:.]+(%[\w.-]+)?$/i

function isIP(addr) {
	return IPV4.test(addr) || (addr.includes(":") && IPV6.test(addr))
}

// parseCustom reads the lines of a -f file: "name;ip", "name;ip;tls-name",
// "name;ip;https-url", or "name;ip;quic://tls-name". It catches the common
// mistakes as the user types. The server checks every field again.
function parseCustom(text) {
	const servers = []
	const problems = []
	text.split("\n").forEach((raw, i) => {
		const line = raw.trim()
		if (!line || line.startsWith("#")) return
		const problem = (reason) => problems.push({ line: i + 1, reason })
		const parts = line.split(";").map((p) => p.trim())
		if (parts.length > 3) return problem("has more than three fields")
		const [name, addr, third] = parts.length === 1 ? ["", parts[0]] : parts
		if (!isIP(addr ?? "")) return problem(`${addr || "the address"} is not an IP address without a port`)
		const server = { name: name || addr, addr }
		if (third?.startsWith("https://")) {
			let url = null
			try {
				url = new URL(third)
			} catch {
				// fall through to the problem below
			}
			if (!url || !url.hostname) return problem("the DoH URL is not a valid https URL")
			server.dohURL = third
		} else if (third?.startsWith("quic://")) {
			const tlsName = third.slice("quic://".length)
			if (!HOSTNAME.test(tlsName)) return problem("the DoQ name after quic:// is not a hostname")
			server.doqName = tlsName
		} else if (third) {
			if (third.startsWith("http://")) return problem("DoH needs https://")
			if (!HOSTNAME.test(third)) return problem("the TLS name is not a hostname")
			server.tlsName = third
		}
		servers.push(server)
	})
	return { servers, problems }
}

// Selection

function isCandidate(e) {
	return (
		setup.family.has(e.family) &&
		setup.transport.has(e.transport) &&
		setup.category.has(e.category) &&
		(!setup.major || e.major) &&
		(!setup.primary || e.primary)
	)
}

function candidatesFor(s) {
	const saved = setup
	setup = s
	try {
		return catalog.filter(isCandidate)
	} finally {
		setup = saved
	}
}

function selectedBuiltins() {
	return catalog.filter((e) => isCandidate(e) && !setup.excluded.has(serverKey(e)))
}

function selectedResolvers() {
	return [...selectedBuiltins().map(wireServer), ...parseCustom($("custom-resolvers").value).servers]
}

function presetSetup(p) {
	return {
		...setup,
		family: new Set(p.family),
		transport: new Set(p.transport),
		category: new Set(p.category),
		major: p.major,
		primary: p.primary,
		excluded: new Set(),
	}
}

function sameSet(a, b) {
	return a.size === b.size && [...a].every((v) => b.has(v))
}

// domainSetOf names the built-in domain list that the textarea holds.
function domainSetOf(domains) {
	for (const [id, list] of Object.entries(DOMAIN_SETS)) {
		if (domains.length === list.length && domains.every((d, i) => d === list[i])) return id
	}
	return null
}

function activePreset() {
	const domains = parseLines($("domains").value)
	return PRESETS.find(
		(p) =>
			sameSet(setup.family, new Set(p.family)) &&
			sameSet(setup.transport, new Set(p.transport)) &&
			sameSet(setup.category, new Set(p.category)) &&
			setup.major === p.major &&
			setup.primary === p.primary &&
			setup.excluded.size === 0 &&
			Number($("repeats").value) === p.repeats &&
			domainSetOf(domains) === p.domains,
	)
}

function applyPreset(p) {
	setup = presetSetup(p)
	// Keep domains the user typed. Swap only a built-in list for another.
	if (domainSetOf(parseLines($("domains").value)) || !$("domains").value.trim()) {
		$("domains").value = DOMAIN_SETS[p.domains].join("\n")
	}
	$("repeats").value = p.repeats
	setupChanged()
}

// Setup panel

function setToggles(groupId, values) {
	for (const b of $(groupId).querySelectorAll("button")) {
		b.setAttribute("aria-pressed", String(values.has(b.dataset.value)))
	}
}

// presetHint is the preset under the pointer or keyboard focus. The line
// under the presets describes it, or the active preset when there is none.
let presetHint = null

function presetCounts(p) {
	const domains = parseLines($("domains").value)
	const keepDomains = !domainSetOf(domains) && domains.length > 0
	const d = keepDomains ? domains.length : DOMAIN_SETS[p.domains].length
	const n = candidatesFor(presetSetup(p)).length
	return `${plural(n, "resolver")} · ${plural(n * d * p.repeats, "lookup")}`
}

function renderPresetDetail() {
	const active = activePreset()
	const p = presetHint ?? active
	const detail = $("preset-detail")
	detail.replaceChildren()
	if (!p) {
		detail.textContent = "Your own setup. Pick a preset to start from one of these."
		return
	}
	detail.append(el("span", { textContent: `${p.about} ` }), el("span", { className: "preset-count", textContent: presetCounts(p) }))
}

function renderPresets() {
	const active = activePreset()
	$("preset-name").textContent = active ? active.name : "custom"
	$("presets").replaceChildren(
		...PRESETS.map((p) => {
			const button = el("button", { type: "button", className: "preset", textContent: p.name })
			button.setAttribute("aria-pressed", String(active?.id === p.id))
			button.setAttribute("aria-describedby", "preset-detail")
			button.addEventListener("click", () => applyPreset(p))
			for (const [on, value] of [["mouseenter", p], ["focus", p], ["mouseleave", null], ["blur", null]]) {
				button.addEventListener(on, () => {
					presetHint = value
					renderPresetDetail()
				})
			}
			return button
		}),
	)
	renderPresetDetail()
}

function matchesSearch(e, terms) {
	if (terms.length === 0) return true
	const hay = `${e.name} ${e.provider} ${e.addr} ${e.category} ${e.transport}`.toLowerCase()
	return terms.every((t) => hay.includes(t))
}

function renderPicker() {
	const terms = setup.search.toLowerCase().split(/\s+/).filter(Boolean)
	const groups = new Map()
	for (const e of catalog) {
		if (!isCandidate(e) || !matchesSearch(e, terms)) continue
		if (!groups.has(e.provider)) groups.set(e.provider, [])
		groups.get(e.provider).push(e)
	}

	if (groups.size === 0) {
		$("builtin-list").replaceChildren(
			el("li", { className: "picker-empty", textContent: terms.length ? "No resolver matches the search." : "No resolver matches these filters. Turn on another address family, transport, or kind." }),
		)
		return
	}

	$("builtin-list").replaceChildren(
		...[...groups].map(([provider, entries]) => {
			const chosen = entries.filter((e) => !setup.excluded.has(serverKey(e))).length
			const open = setup.open.has(provider) || terms.length > 0
			const box = el("input", { type: "checkbox", checked: chosen === entries.length, indeterminate: chosen > 0 && chosen < entries.length })
			box.dataset.provider = provider
			box.setAttribute("aria-label", `Select all of ${provider}`)
			const transports = [...new Set(entries.map((e) => e.transport))]
			const toggle = el(
				"button",
				{ type: "button", className: "group-name" },
				el("span", { className: "caret", textContent: open ? "▾" : "▸" }),
				el("span", { className: "name", textContent: provider }),
			)
			toggle.dataset.open = provider
			toggle.title = `${provider}, ${entries[0].category}`
			toggle.setAttribute("aria-expanded", String(open))
			return el(
				"li",
				{ className: "group" },
				el(
					"div",
					{ className: "group-head" },
					box,
					toggle,
					el(
						"span",
						{ className: "group-meta" },
						...transports.filter((t) => t !== "plain").map((t) => el("span", { className: `tag tag-${t}`, textContent: TRANSPORT_LABEL[t] })),
						el("span", { className: "count", textContent: `${chosen}/${entries.length}` }),
					),
				),
				open
					? el(
							"ul",
							{ className: "members" },
							...entries.map((e) => {
								const key = serverKey(e)
								const member = el("input", { type: "checkbox", checked: !setup.excluded.has(key) })
								member.dataset.key = key
								return el(
									"li",
									{},
									el(
										"label",
										{ title: `${e.name}, ${e.addr}, ${transportLabel(e)}` },
										member,
										el("span", { className: "name", textContent: e.name }),
										el("span", { className: "addr", textContent: e.addr }),
									),
								)
							}),
						)
					: null,
			)
		}),
	)
}

function renderCustom() {
	const { servers, problems } = parseCustom($("custom-resolvers").value)
	$("custom-count").textContent = servers.length || problems.length ? `${servers.length} added${problems.length ? `, ${plural(problems.length, "problem")}` : ""}` : ""
	$("custom-problems").replaceChildren(...problems.map((p) => el("li", { textContent: `Line ${p.line}: ${p.reason}.` })))
}

function renderConfig() {
	setToggles("filter-family", setup.family)
	setToggles("filter-transport", setup.transport)
	setToggles("filter-category", setup.category)
	$("only-major").checked = setup.major
	$("primary-only").checked = setup.primary
	$("resolver-search").value = setup.search

	const domains = parseLines($("domains").value)
	$("domain-count").textContent = domains.length
	setToggles("domain-sets", new Set([domainSetOf(domains)]))

	const candidates = catalog.filter(isCandidate)
	const builtinChosen = selectedBuiltins().length
	$("resolver-count").textContent = `${builtinChosen} of ${candidates.length} selected`

	renderPresets()
	renderPicker()
	renderCustom()
	renderEstimate()
}

function estimate() {
	const resolvers = selectedResolvers().length
	const domains = parseLines($("domains").value).length
	const repeats = Math.max(1, Number($("repeats").value) || 1)
	const warmup = Math.max(0, Number($("warmup").value) || 0)
	return { resolvers, domains, repeats, lookups: resolvers * domains * repeats, warmups: resolvers * domains * repeats * warmup }
}

function renderEstimate() {
	const e = estimate()
	let text = `${plural(e.resolvers, "resolver")} × ${plural(e.domains, "domain")} × ${e.repeats} = ${plural(e.lookups, "lookup")}`
	if (e.warmups) text += `, plus ${plural(e.warmups, "warmup lookup")}`
	$("estimate").textContent = text
	if (state.status !== "running" && !state.pending) queueRender()
}

// Persistence. The setup survives a reload; Reset clears it.

function saveSetup() {
	const data = {
		family: [...setup.family],
		transport: [...setup.transport],
		category: [...setup.category],
		major: setup.major,
		primary: setup.primary,
		excluded: [...setup.excluded],
		domains: $("domains").value,
		custom: $("custom-resolvers").value,
		repeats: $("repeats").value,
		timeout: $("timeout").value,
		concurrency: $("concurrency").value,
		warmup: $("warmup").value,
		sort: $("sort").value,
	}
	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(data))
	} catch {
		// Private browsing can refuse storage. The setup still works.
	}
}

function loadSetup() {
	let data = null
	try {
		data = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null")
	} catch {
		return
	}
	if (!data) return
	const only = (values, all) => new Set((values ?? []).filter((v) => all.includes(v)))
	const family = only(data.family, ALL.family)
	const transport = only(data.transport, ALL.transport)
	const category = only(data.category, ALL.category)
	if (family.size) setup.family = family
	if (transport.size) setup.transport = transport
	if (category.size) setup.category = category
	setup.major = Boolean(data.major)
	setup.primary = Boolean(data.primary)
	setup.excluded = new Set(data.excluded ?? [])
	for (const [id, key] of [["domains", "domains"], ["custom-resolvers", "custom"], ["repeats", "repeats"], ["timeout", "timeout"], ["concurrency", "concurrency"], ["warmup", "warmup"], ["sort", "sort"]]) {
		if (typeof data[key] === "string") $(id).value = data[key]
	}
	if (data.custom?.trim()) $("custom-block").open = true
}

function setupChanged() {
	saveSetup()
	renderConfig()
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
	const e = estimate()
	$("status").textContent = state.status
	$("status").dataset.status = state.status
	$("start").disabled = running || state.pending || e.lookups === 0
	$("stop").disabled = !running
	$("setup-fields").disabled = running || state.pending

	renderProgress(e)
	renderSummary()
	renderLadder()
	renderLog()
}

function renderProgress(e) {
	let pct = 0
	if (state.plannedLookups) {
		pct = Math.min(100, (state.lookups / state.plannedLookups) * 100)
	} else if (state.totalResolvers) {
		pct = (state.doneResolvers / state.totalResolvers) * 100
	}
	if (state.status === "complete") pct = 100
	$("progress-fill").style.width = `${pct}%`

	let line
	const elapsed = Date.now() - state.startedAt
	if (state.status === "running" && state.active) {
		line = `Resolver ${Math.min(state.doneResolvers + 1, state.totalResolvers)} of ${state.totalResolvers}: ${state.active} · ${formatDuration(elapsed)}`
		if (state.plannedLookups && state.lookups > 20 && elapsed > 3000) {
			const left = ((state.plannedLookups - state.lookups) * elapsed) / state.lookups
			line += `, about ${formatDuration(left)} left`
		}
	} else if (state.status === "running" || state.pending) {
		line = "Starting…"
	} else if (state.status === "complete") {
		line = `Finished ${plural(state.totalResolvers, "resolver")} and ${plural(state.lookups, "lookup")} in ${formatDuration(elapsed)}.`
	} else if (state.status === "stopped") {
		line = `Stopped after ${state.doneResolvers} of ${plural(state.totalResolvers, "resolver")}.`
	} else if (state.status === "error") {
		line = "The run ended with an error."
	} else if (e.lookups === 0) {
		line = e.resolvers === 0 ? "Select at least one resolver." : "Add at least one domain."
	} else {
		line = `Ready: ${plural(e.resolvers, "resolver")}, ${plural(e.domains, "domain")}, ${plural(e.lookups, "lookup")}.`
	}
	$("run-line").textContent = line
	document.title = state.status === "running" ? `${Math.floor(pct)}% · dnsbench` : "dnsbench"
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
		["no answer at all", failed ? plural(failed, "resolver") : "none"],
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

const rowNodes = new Map() // serverKey -> {li, head, canvas, p50, p95, ok, errline, detail}
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
	const errline = el("span", { className: "errline" })
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
		errline,
	)
	head.setAttribute("aria-expanded", "false")
	const nodes = {
		li: el("li", { className: "row" }),
		head,
		canvas,
		errline,
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

function renderLadderFilter(entries) {
	const present = [...new Set(entries.map((e) => transportOf(e.server)))]
	const bar = $("ladder-transport")
	bar.hidden = present.length < 2
	if (bar.hidden) return
	bar.replaceChildren(
		...ALL.transport
			.filter((t) => present.includes(t))
			.map((t) => {
				const b = el("button", { type: "button", textContent: TRANSPORT_LABEL[t], title: `Show ${TRANSPORT_LABEL[t]} resolvers` })
				b.dataset.value = t
				b.setAttribute("aria-pressed", String(!state.hiddenTransports.has(t)))
				return b
			}),
	)
}

function renderLadder() {
	const entries = [...state.results.values()]
	$("ladder-empty").hidden = entries.length > 0
	$("export-csv").disabled = entries.length === 0
	$("export-json").disabled = entries.length === 0
	document.body.classList.toggle("has-results", entries.length > 0)
	renderLadderFilter(entries)

	const shown = entries.filter((e) => !state.hiddenTransports.has(transportOf(e.server)))
	const ordered = shown.sort(compareEntries($("sort").value))
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
		const topError = [...entry.errors.entries()].sort((a, b) => b[1] - a[1])[0]
		nodes.errline.textContent = topError ? `${topError[1]}× ${topError[0]}` : ""
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
	state.hiddenTransports = new Set()
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
			state.startedAt = Date.now()
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
	$("custom-block").open = false
	try {
		localStorage.removeItem(STORAGE_KEY)
	} catch {
		// Nothing was saved.
	}
	setup = defaultSetup()
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
	if (state.status === "running" || state.pending) return
	const domains = parseLines($("domains").value)
	if (domains.length === 0) {
		showNotice("Add at least one domain.")
		return
	}
	const custom = parseCustom($("custom-resolvers").value)
	if (custom.problems.length) {
		const p = custom.problems[0]
		$("custom-block").open = true
		showNotice(`Fix your own resolvers first. Line ${p.line}: ${p.reason}.`)
		return
	}
	const resolvers = selectedResolvers()
	if (resolvers.length === 0) {
		showNotice("Select at least one resolver.")
		return
	}

	const options = {
		repeats: Number($("repeats").value),
		timeoutMs: Number($("timeout").value),
		concurrency: Number($("concurrency").value),
		warmup: Number($("warmup").value),
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
		if (state.status !== "running") {
			state.status = "running"
			state.startedAt = Date.now()
		}
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

// Wiring

function toggleIn(set, value, allowEmpty = false) {
	if (set.has(value)) {
		if (set.size > 1 || allowEmpty) set.delete(value)
	} else {
		set.add(value)
	}
}

for (const [groupId, key] of [["filter-family", "family"], ["filter-transport", "transport"], ["filter-category", "category"]]) {
	$(groupId).addEventListener("click", (event) => {
		const value = event.target.closest("button")?.dataset.value
		if (!value) return
		toggleIn(setup[key], value)
		setupChanged()
	})
}

$("domain-sets").addEventListener("click", (event) => {
	const value = event.target.closest("button")?.dataset.value
	if (!value) return
	$("domains").value = DOMAIN_SETS[value].join("\n")
	setupChanged()
})

$("only-major").addEventListener("change", () => {
	setup.major = $("only-major").checked
	setupChanged()
})

$("primary-only").addEventListener("change", () => {
	setup.primary = $("primary-only").checked
	setupChanged()
})

$("resolver-search").addEventListener("input", () => {
	setup.search = $("resolver-search").value
	renderPicker()
})

// The picker re-renders on every change, so handle its checkboxes and
// buttons here, before anything replaces them.
$("builtin-list").addEventListener("change", (event) => {
	const box = event.target
	if (box.dataset.key) {
		if (box.checked) setup.excluded.delete(box.dataset.key)
		else setup.excluded.add(box.dataset.key)
	} else if (box.dataset.provider) {
		for (const e of catalog.filter((c) => c.provider === box.dataset.provider && isCandidate(c))) {
			if (box.checked) setup.excluded.delete(serverKey(e))
			else setup.excluded.add(serverKey(e))
		}
	}
	setupChanged()
})

$("builtin-list").addEventListener("click", (event) => {
	const provider = event.target.closest("button[data-open]")?.dataset.open
	if (!provider) return
	toggleIn(setup.open, provider, true)
	renderPicker()
})

$("select-all").addEventListener("click", () => {
	for (const e of catalog.filter(isCandidate)) setup.excluded.delete(serverKey(e))
	setupChanged()
})

$("select-none").addEventListener("click", () => {
	for (const e of catalog.filter(isCandidate)) setup.excluded.add(serverKey(e))
	setupChanged()
})

// Text and number fields: re-render on input, but not for the picker,
// whose own handlers run on change.
form.addEventListener("input", (event) => {
	if (event.target.closest("#builtin-list") || event.target.id === "resolver-search") return
	setupChanged()
})

$("timeout").addEventListener("change", () => {
	if (state.status !== "running") {
		state.axisMaxMs = Math.max(1000, Number($("timeout").value) || 3000)
		renderAxis()
		markAllDirty()
	}
})

$("ladder-transport").addEventListener("click", (event) => {
	const value = event.target.closest("button")?.dataset.value
	if (!value) return
	const present = new Set([...state.results.values()].map((e) => transportOf(e.server)))
	// Keep at least one transport visible.
	if (!state.hiddenTransports.has(value) && [...present].filter((t) => !state.hiddenTransports.has(t)).length === 1) return
	toggleIn(state.hiddenTransports, value, true)
	queueRender()
})

form.addEventListener("submit", startRun)
$("stop").addEventListener("click", stopRun)
$("reset").addEventListener("click", resetRun)
$("sort").addEventListener("change", () => {
	saveSetup()
	queueRender()
})
$("export-csv").addEventListener("click", exportCSV)
$("export-json").addEventListener("click", exportJSON)
document.querySelector("details.log").addEventListener("toggle", queueRender)

document.addEventListener("keydown", (event) => {
	if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
		event.preventDefault()
		if (!$("start").disabled) form.requestSubmit()
		return
	}
	const typing = event.target.closest("input, textarea, select")
	if (event.key === "/" && !typing) {
		event.preventDefault()
		$("resolver-search").focus()
	}
})

window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
	readPalette()
	markAllDirty()
})
new ResizeObserver(markAllDirty).observe($("ladder"))

// The axis header sticks under the masthead, whose height changes with
// the window width.
function trackMasthead() {
	document.documentElement.style.setProperty("--masthead-h", `${document.querySelector(".masthead").offsetHeight}px`)
}
new ResizeObserver(trackMasthead).observe(document.querySelector(".masthead"))

// While a run goes, tick the elapsed time even when no event arrives.
setInterval(() => {
	if (state.status === "running") queueRender()
}, 1000)

loadSetup()
state.axisMaxMs = Math.max(1000, Number($("timeout").value) || 3000)
readPalette()
renderAxis()
renderConfig()
render()
connectEvents()
