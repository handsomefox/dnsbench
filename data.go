package main

// provider is one DNS service in the built-in list. Every address becomes
// one resolver, named after the provider and its position in its list:
// Cloudflare-1 and Cloudflare-2 for IPv4, Cloudflare-v6-1 and
// Cloudflare-v6-2 for IPv6.
//
// The addresses were probed with plain DNS over UDP and TCP from a
// dual-stack host on 2026-09-23.
type provider struct {
	name  string
	major bool // included by -major
	ipv4  []string
	ipv6  []string
}

var providers = []provider{
	// Major providers
	{name: "Cloudflare", major: true, ipv4: []string{"1.1.1.1", "1.0.0.1"}, ipv6: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}},
	{name: "Google", major: true, ipv4: []string{"8.8.8.8", "8.8.4.4"}, ipv6: []string{"2001:4860:4860::8888", "2001:4860:4860::8844"}},
	{name: "Quad9", major: true, ipv4: []string{"9.9.9.9", "149.112.112.112"}, ipv6: []string{"2620:fe::fe", "2620:fe::9"}},
	{name: "Quad9-ECS", major: true, ipv4: []string{"9.9.9.11", "149.112.112.11"}, ipv6: []string{"2620:fe::11", "2620:fe::fe:11"}},
	{name: "OpenDNS", ipv4: []string{"208.67.222.222", "208.67.220.220"}, ipv6: []string{"2620:119:35::35", "2620:119:53::53"}},

	// Ad-blocking and filtering
	{name: "AdGuard", major: true, ipv4: []string{"94.140.14.14", "94.140.15.15"}, ipv6: []string{"2a10:50c0::ad1:ff", "2a10:50c0::ad2:ff"}},
	{name: "CleanBrowsing", ipv4: []string{"185.228.168.9", "185.228.169.9"}, ipv6: []string{"2a0d:2a00:1::2", "2a0d:2a00:2::2"}},
	{name: "NextDNS", major: true, ipv4: []string{"45.90.28.0", "45.90.30.0"}, ipv6: []string{"2a07:a8c0::", "2a07:a8c1::"}},
	{name: "ControlD", ipv4: []string{"76.76.2.0", "76.76.10.0"}, ipv6: []string{"2606:1a40::", "2606:1a40:1::"}},

	// Privacy-focused
	{name: "UncensoredDNS", ipv4: []string{"91.239.100.100", "89.233.43.71"}, ipv6: []string{"2001:67c:28a4::", "2a01:3a0:53:53::"}},

	// Regional/National
	{name: "AliDNS", ipv4: []string{"223.5.5.5", "223.6.6.6"}, ipv6: []string{"2400:3200::1", "2400:3200:baba::1"}},
	{name: "DNSPod", ipv4: []string{"119.29.29.29", "119.28.28.28"}, ipv6: []string{"2402:4e00::"}},
	{name: "Canadian-Shield", ipv4: []string{"149.112.121.10", "149.112.122.10"}, ipv6: []string{"2620:10a:80bb::10", "2620:10a:80bc::10"}},

	// Alternative providers
	{name: "DNS-SB", ipv4: []string{"185.222.222.222", "45.11.45.11"}, ipv6: []string{"2a09::", "2a11::"}},
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
