package main

// provider is one DNS service in the built-in list. Every address becomes
// one resolver, named after the provider and its position in its list:
// Cloudflare-1 and Cloudflare-2 for IPv4, Cloudflare-v6-1 for IPv6. A
// provider with a tlsName also serves DNS over TLS on the same addresses,
// as Cloudflare-DoT-1 and Cloudflare-DoT-v6-1.
//
// Every address and transport listed here answered from a dual-stack host
// on 2026-09-23. To check them again, run:
//
//	DNSBENCH_LIVE=1 go test -run TestBuiltinServersAnswer -v
type provider struct {
	name          string
	major         bool // included by -major
	ipv4          []string
	ipv6          []string
	tlsName       string // DoT certificate name. Empty when the provider has no DoT.
	dohURL        string // DoH endpoint. Empty when the provider has no DoH.
	doqName       string // DoQ certificate name. Empty when the provider has no DoQ.
	encryptedOnly bool   // the addresses answer DoT, DoH, or DoQ but not plain DNS
}

var providers = []provider{
	// Major providers
	{name: "Cloudflare", major: true, ipv4: []string{"1.1.1.1", "1.0.0.1"}, ipv6: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}, tlsName: "cloudflare-dns.com", dohURL: "https://cloudflare-dns.com/dns-query"},
	{name: "Google", major: true, ipv4: []string{"8.8.8.8", "8.8.4.4"}, ipv6: []string{"2001:4860:4860::8888", "2001:4860:4860::8844"}, tlsName: "dns.google", dohURL: "https://dns.google/dns-query"},
	{name: "Quad9", major: true, ipv4: []string{"9.9.9.9", "149.112.112.112"}, ipv6: []string{"2620:fe::fe", "2620:fe::9"}, tlsName: "dns.quad9.net", dohURL: "https://dns.quad9.net/dns-query", doqName: "dns.quad9.net"},
	{name: "Quad9-ECS", major: true, ipv4: []string{"9.9.9.11", "149.112.112.11"}, ipv6: []string{"2620:fe::11", "2620:fe::fe:11"}, tlsName: "dns11.quad9.net", dohURL: "https://dns11.quad9.net/dns-query", doqName: "dns11.quad9.net"},
	{name: "OpenDNS", ipv4: []string{"208.67.222.222", "208.67.220.220"}, ipv6: []string{"2620:119:35::35", "2620:119:53::53"}, tlsName: "dns.opendns.com", dohURL: "https://doh.opendns.com/dns-query"},

	// Ad-blocking and filtering
	{name: "AdGuard", major: true, ipv4: []string{"94.140.14.14", "94.140.15.15"}, ipv6: []string{"2a10:50c0::ad1:ff", "2a10:50c0::ad2:ff"}, tlsName: "dns.adguard-dns.com", dohURL: "https://dns.adguard-dns.com/dns-query", doqName: "dns.adguard-dns.com"},
	{name: "CleanBrowsing", ipv4: []string{"185.228.168.9", "185.228.169.9"}, ipv6: []string{"2a0d:2a00:1::2", "2a0d:2a00:2::2"}, tlsName: "security-filter-dns.cleanbrowsing.org"},
	{name: "NextDNS", major: true, ipv4: []string{"45.90.28.0", "45.90.30.0"}, ipv6: []string{"2a07:a8c0::", "2a07:a8c1::"}, tlsName: "dns.nextdns.io", dohURL: "https://dns.nextdns.io/dns-query", doqName: "dns.nextdns.io"},
	{name: "ControlD", ipv4: []string{"76.76.2.0", "76.76.10.0"}, ipv6: []string{"2606:1a40::", "2606:1a40:1::"}, dohURL: "https://freedns.controld.com/p0"},

	// Privacy-focused
	{name: "Mullvad", ipv4: []string{"194.242.2.2"}, ipv6: []string{"2a07:e340::2"}, tlsName: "dns.mullvad.net", encryptedOnly: true, dohURL: "https://dns.mullvad.net/dns-query"},
	{name: "Mullvad-Adblock", ipv4: []string{"194.242.2.3"}, ipv6: []string{"2a07:e340::3"}, tlsName: "adblock.dns.mullvad.net", encryptedOnly: true, dohURL: "https://adblock.dns.mullvad.net/dns-query"},
	{name: "UncensoredDNS", ipv4: []string{"91.239.100.100"}, ipv6: []string{"2001:67c:28a4::"}, tlsName: "anycast.uncensoreddns.org", encryptedOnly: true, dohURL: "https://anycast.uncensoreddns.org/dns-query"},
	{name: "UncensoredDNS-Unicast", ipv4: []string{"89.233.43.71"}, ipv6: []string{"2a01:3a0:53:53::"}, tlsName: "unicast.uncensoreddns.org", encryptedOnly: true, dohURL: "https://unicast.uncensoreddns.org/dns-query"},
	{name: "LibreDNS", ipv4: []string{"116.202.176.26"}, tlsName: "dot.libredns.gr", encryptedOnly: true, dohURL: "https://doh.libredns.gr/dns-query"},

	// Regional/National
	{name: "AliDNS", ipv4: []string{"223.5.5.5", "223.6.6.6"}, ipv6: []string{"2400:3200::1", "2400:3200:baba::1"}, tlsName: "dns.alidns.com", dohURL: "https://dns.alidns.com/dns-query", doqName: "dns.alidns.com"},
	{name: "DNSPod", ipv4: []string{"119.29.29.29", "119.28.28.28"}, ipv6: []string{"2402:4e00::"}},
	{name: "Canadian-Shield", ipv4: []string{"149.112.121.10", "149.112.122.10"}, ipv6: []string{"2620:10a:80bb::10", "2620:10a:80bc::10"}, tlsName: "private.canadianshield.cira.ca", dohURL: "https://private.canadianshield.cira.ca/dns-query"},

	// Alternative providers
	{name: "DNS-SB", ipv4: []string{"185.222.222.222", "45.11.45.11"}, ipv6: []string{"2a09::", "2a11::"}, tlsName: "dot.sb", dohURL: "https://doh.dns.sb/dns-query"},
}

var (
	defaultSites = []string{
		// Search engines
		"google.com", "bing.com", "duckduckgo.com", "yahoo.com",

		// Knowledge & reference
		"wikipedia.org", "archive.org", "stackoverflow.com",
		"github.com", "gitlab.com",

		// Programming languages
		"python.org", "golang.org", "nodejs.org", "rust-lang.org",

		// Major news outlets
		"nytimes.com", "bbc.com", "cnn.com", "reuters.com",
		"theguardian.com", "bloomberg.com",

		// E-commerce
		"amazon.com", "ebay.com", "etsy.com", "shopify.com",

		// Streaming & entertainment
		"youtube.com", "netflix.com", "spotify.com", "vimeo.com",

		// Social & communication
		"linkedin.com", "zoom.us", "slack.com",

		// Cloud & tech
		"cloudflare.com", "aws.amazon.com", "microsoft.com",

		// Finance
		"paypal.com", "stripe.com", "visa.com",

		// Government & organizations
		"usa.gov", "europa.eu", "un.org", "nasa.gov",

		// Health & science
		"nih.gov", "cdc.gov", "mayoclinic.org",

		// Travel
		"booking.com", "airbnb.com", "expedia.com",

		// Additional popular sites
		"reddit.com", "twitter.com", "facebook.com", "instagram.com",
		"tiktok.com", "pinterest.com", "wordpress.com", "medium.com",
	}
)
