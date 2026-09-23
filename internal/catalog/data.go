package catalog

// service is one DNS service in the built-in list, such as Cloudflare or
// Cloudflare-Family. Its provider is the company that runs it, and its
// name starts with the provider's. Every address becomes one resolver,
// named after the service and its position in its list: Cloudflare-1 and
// Cloudflare-2 for IPv4, Cloudflare-v6-1 for IPv6. A service with a tlsName
// also serves DNS over TLS on the same addresses, as Cloudflare-DoT-1 and
// Cloudflare-DoT-v6-1.
//
// Every address and transport listed here answered the live test below when
// it was added. To check them again, run:
//
//	DNSBENCH_LIVE=1 go test ./internal/catalog -run TestBuiltinServersAnswer -v
type service struct {
	name          string
	provider      string // the company, such as Cloudflare for Cloudflare-Family
	category      string // Global, Filtering, Privacy, or Regional, for the dashboard
	major         bool   // included by -major
	ipv4          []string
	ipv6          []string
	tlsName       string // DoT certificate name. Empty when the service has no DoT.
	dohURL        string // DoH endpoint. Empty when the service has no DoH.
	doqName       string // DoQ certificate name. Empty when the service has no DoQ.
	encryptedOnly bool   // the addresses answer DoT, DoH, or DoQ but not plain DNS
}

var services = []service{
	// Large public resolvers without filtering.
	{name: "Cloudflare", provider: "Cloudflare", category: "Global", major: true, ipv4: []string{"1.1.1.1", "1.0.0.1"}, ipv6: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"}, tlsName: "cloudflare-dns.com", dohURL: "https://cloudflare-dns.com/dns-query"},
	{name: "Google", provider: "Google", category: "Global", major: true, ipv4: []string{"8.8.8.8", "8.8.4.4"}, ipv6: []string{"2001:4860:4860::8888", "2001:4860:4860::8844"}, tlsName: "dns.google", dohURL: "https://dns.google/dns-query"},
	{name: "NextDNS", provider: "NextDNS", category: "Global", major: true, ipv4: []string{"45.90.28.0", "45.90.30.0"}, ipv6: []string{"2a07:a8c0::", "2a07:a8c1::"}, tlsName: "dns.nextdns.io", dohURL: "https://dns.nextdns.io/dns-query", doqName: "dns.nextdns.io"},
	{name: "OpenDNS", provider: "OpenDNS", category: "Global", ipv4: []string{"208.67.222.222", "208.67.220.220"}, ipv6: []string{"2620:119:35::35", "2620:119:53::53"}, tlsName: "dns.opendns.com", dohURL: "https://doh.opendns.com/dns-query"},
	{name: "DNS4EU", provider: "DNS4EU", category: "Global", ipv4: []string{"86.54.11.100", "86.54.11.200"}, ipv6: []string{"2a13:1001::86:54:11:100", "2a13:1001::86:54:11:200"}, tlsName: "unfiltered.joindns4.eu", dohURL: "https://unfiltered.joindns4.eu/dns-query"},
	{name: "ControlD", provider: "ControlD", category: "Global", ipv4: []string{"76.76.2.0", "76.76.10.0"}, ipv6: []string{"2606:1a40::", "2606:1a40:1::"}, dohURL: "https://freedns.controld.com/p0"},
	{name: "DNS-SB", provider: "DNS-SB", category: "Global", ipv4: []string{"185.222.222.222", "45.11.45.11"}, ipv6: []string{"2a09::", "2a11::"}, tlsName: "dot.sb", dohURL: "https://doh.dns.sb/dns-query"},
	{name: "AdGuard-Unfiltered", provider: "AdGuard", category: "Global", ipv4: []string{"94.140.14.140", "94.140.14.141"}, ipv6: []string{"2a10:50c0::1:ff", "2a10:50c0::2:ff"}, tlsName: "unfiltered.adguard-dns.com", dohURL: "https://unfiltered.adguard-dns.com/dns-query", doqName: "unfiltered.adguard-dns.com"},
	{name: "Quad9-Unfiltered", provider: "Quad9", category: "Global", ipv4: []string{"9.9.9.10", "149.112.112.10"}, ipv6: []string{"2620:fe::10", "2620:fe::fe:10"}, tlsName: "dns10.quad9.net", dohURL: "https://dns10.quad9.net/dns-query", doqName: "dns10.quad9.net"},

	// Resolvers that block malware, ads, trackers, or adult content. Quad9
	// blocks malware on its main addresses, so it is here too.
	{name: "Quad9", provider: "Quad9", category: "Filtering", major: true, ipv4: []string{"9.9.9.9", "149.112.112.112"}, ipv6: []string{"2620:fe::fe", "2620:fe::9"}, tlsName: "dns.quad9.net", dohURL: "https://dns.quad9.net/dns-query", doqName: "dns.quad9.net"},
	{name: "Quad9-ECS", provider: "Quad9", category: "Filtering", major: true, ipv4: []string{"9.9.9.11", "149.112.112.11"}, ipv6: []string{"2620:fe::11", "2620:fe::fe:11"}, tlsName: "dns11.quad9.net", dohURL: "https://dns11.quad9.net/dns-query", doqName: "dns11.quad9.net"},
	{name: "AdGuard", provider: "AdGuard", category: "Filtering", major: true, ipv4: []string{"94.140.14.14", "94.140.15.15"}, ipv6: []string{"2a10:50c0::ad1:ff", "2a10:50c0::ad2:ff"}, tlsName: "dns.adguard-dns.com", dohURL: "https://dns.adguard-dns.com/dns-query", doqName: "dns.adguard-dns.com"},
	{name: "AdGuard-Family", provider: "AdGuard", category: "Filtering", ipv4: []string{"94.140.14.15", "94.140.15.16"}, ipv6: []string{"2a10:50c0::bad1:ff", "2a10:50c0::bad2:ff"}, tlsName: "family.adguard-dns.com", dohURL: "https://family.adguard-dns.com/dns-query", doqName: "family.adguard-dns.com"},
	// The Cloudflare filters serve DoH without the intermediate certificate,
	// which browsers fetch but Go does not, so only their DoT is listed.
	{name: "Cloudflare-Security", provider: "Cloudflare", category: "Filtering", ipv4: []string{"1.1.1.2", "1.0.0.2"}, ipv6: []string{"2606:4700:4700::1112", "2606:4700:4700::1002"}, tlsName: "security.cloudflare-dns.com"},
	{name: "Cloudflare-Family", provider: "Cloudflare", category: "Filtering", ipv4: []string{"1.1.1.3", "1.0.0.3"}, ipv6: []string{"2606:4700:4700::1113", "2606:4700:4700::1003"}, tlsName: "family.cloudflare-dns.com"},
	{name: "OpenDNS-FamilyShield", provider: "OpenDNS", category: "Filtering", ipv4: []string{"208.67.222.123", "208.67.220.123"}, ipv6: []string{"2620:119:35::123", "2620:119:53::123"}, tlsName: "familyshield.opendns.com", dohURL: "https://doh.familyshield.opendns.com/dns-query"},
	// CleanBrowsing serves DoH on only some of these addresses, so only its
	// DoT is listed.
	{name: "CleanBrowsing", provider: "CleanBrowsing", category: "Filtering", ipv4: []string{"185.228.168.9", "185.228.169.9"}, ipv6: []string{"2a0d:2a00:1::2", "2a0d:2a00:2::2"}, tlsName: "security-filter-dns.cleanbrowsing.org"},
	{name: "CleanBrowsing-Family", provider: "CleanBrowsing", category: "Filtering", ipv4: []string{"185.228.168.168", "185.228.169.168"}, ipv6: []string{"2a0d:2a00:1::", "2a0d:2a00:2::"}, tlsName: "family-filter-dns.cleanbrowsing.org"},
	{name: "CleanBrowsing-Adult", provider: "CleanBrowsing", category: "Filtering", ipv4: []string{"185.228.168.10", "185.228.169.11"}, ipv6: []string{"2a0d:2a00:1::1", "2a0d:2a00:2::1"}, tlsName: "adult-filter-dns.cleanbrowsing.org"},
	{name: "DNS4EU-Protective", provider: "DNS4EU", category: "Filtering", ipv4: []string{"86.54.11.1", "86.54.11.201"}, ipv6: []string{"2a13:1001::86:54:11:1", "2a13:1001::86:54:11:201"}, tlsName: "protective.joindns4.eu", dohURL: "https://protective.joindns4.eu/dns-query"},
	{name: "DNS4EU-Child", provider: "DNS4EU", category: "Filtering", ipv4: []string{"86.54.11.12", "86.54.11.212"}, ipv6: []string{"2a13:1001::86:54:11:12", "2a13:1001::86:54:11:212"}, tlsName: "child.joindns4.eu", dohURL: "https://child.joindns4.eu/dns-query"},
	{name: "DNS4EU-NoAds", provider: "DNS4EU", category: "Filtering", ipv4: []string{"86.54.11.13", "86.54.11.213"}, ipv6: []string{"2a13:1001::86:54:11:13", "2a13:1001::86:54:11:213"}, tlsName: "noads.joindns4.eu", dohURL: "https://noads.joindns4.eu/dns-query"},
	{name: "DNS4EU-Child-NoAds", provider: "DNS4EU", category: "Filtering", ipv4: []string{"86.54.11.11", "86.54.11.211"}, ipv6: []string{"2a13:1001::86:54:11:11", "2a13:1001::86:54:11:211"}, tlsName: "child-noads.joindns4.eu", dohURL: "https://child-noads.joindns4.eu/dns-query"},
	{name: "Canadian-Shield-Protected", provider: "Canadian-Shield", category: "Filtering", ipv4: []string{"149.112.121.20", "149.112.122.20"}, ipv6: []string{"2620:10a:80bb::20", "2620:10a:80bc::20"}, tlsName: "protected.canadianshield.cira.ca", dohURL: "https://protected.canadianshield.cira.ca/dns-query"},
	{name: "Canadian-Shield-Family", provider: "Canadian-Shield", category: "Filtering", ipv4: []string{"149.112.121.30", "149.112.122.30"}, ipv6: []string{"2620:10a:80bb::30", "2620:10a:80bc::30"}, tlsName: "family.canadianshield.cira.ca", dohURL: "https://family.canadianshield.cira.ca/dns-query"},
	// ControlD serves DoT on other addresses than these, so only its DoH is
	// listed, as for ControlD above.
	{name: "ControlD-Malware", provider: "ControlD", category: "Filtering", ipv4: []string{"76.76.2.1", "76.76.10.1"}, ipv6: []string{"2606:1a40::1", "2606:1a40:1::1"}, dohURL: "https://freedns.controld.com/p1"},
	{name: "ControlD-Ads", provider: "ControlD", category: "Filtering", ipv4: []string{"76.76.2.2", "76.76.10.2"}, ipv6: []string{"2606:1a40::2", "2606:1a40:1::2"}, dohURL: "https://freedns.controld.com/p2"},
	{name: "ControlD-Social", provider: "ControlD", category: "Filtering", ipv4: []string{"76.76.2.3", "76.76.10.3"}, ipv6: []string{"2606:1a40::3", "2606:1a40:1::3"}, dohURL: "https://freedns.controld.com/p3"},
	{name: "ControlD-Family", provider: "ControlD", category: "Filtering", ipv4: []string{"76.76.2.4", "76.76.10.4"}, ipv6: []string{"2606:1a40::4", "2606:1a40:1::4"}, dohURL: "https://freedns.controld.com/family"},
	{name: "Mullvad-Adblock", provider: "Mullvad", category: "Filtering", ipv4: []string{"194.242.2.3"}, ipv6: []string{"2a07:e340::3"}, tlsName: "adblock.dns.mullvad.net", dohURL: "https://adblock.dns.mullvad.net/dns-query", encryptedOnly: true},
	{name: "Mullvad-Base", provider: "Mullvad", category: "Filtering", ipv4: []string{"194.242.2.4"}, ipv6: []string{"2a07:e340::4"}, tlsName: "base.dns.mullvad.net", dohURL: "https://base.dns.mullvad.net/dns-query", encryptedOnly: true},
	{name: "Mullvad-Extended", provider: "Mullvad", category: "Filtering", ipv4: []string{"194.242.2.5"}, ipv6: []string{"2a07:e340::5"}, tlsName: "extended.dns.mullvad.net", dohURL: "https://extended.dns.mullvad.net/dns-query", encryptedOnly: true},
	{name: "Mullvad-Family", provider: "Mullvad", category: "Filtering", ipv4: []string{"194.242.2.6"}, ipv6: []string{"2a07:e340::6"}, tlsName: "family.dns.mullvad.net", dohURL: "https://family.dns.mullvad.net/dns-query", encryptedOnly: true},
	{name: "Mullvad-All", provider: "Mullvad", category: "Filtering", ipv4: []string{"194.242.2.9"}, ipv6: []string{"2a07:e340::9"}, tlsName: "all.dns.mullvad.net", dohURL: "https://all.dns.mullvad.net/dns-query", encryptedOnly: true},

	// Small operators that promise no logging, mostly encrypted only.
	{name: "Mullvad", provider: "Mullvad", category: "Privacy", ipv4: []string{"194.242.2.2"}, ipv6: []string{"2a07:e340::2"}, tlsName: "dns.mullvad.net", dohURL: "https://dns.mullvad.net/dns-query", encryptedOnly: true},
	{name: "UncensoredDNS", provider: "UncensoredDNS", category: "Privacy", ipv4: []string{"91.239.100.100"}, ipv6: []string{"2001:67c:28a4::"}, tlsName: "anycast.uncensoreddns.org", dohURL: "https://anycast.uncensoreddns.org/dns-query", encryptedOnly: true},
	{name: "UncensoredDNS-Unicast", provider: "UncensoredDNS", category: "Privacy", ipv4: []string{"89.233.43.71"}, ipv6: []string{"2a01:3a0:53:53::"}, tlsName: "unicast.uncensoreddns.org", dohURL: "https://unicast.uncensoreddns.org/dns-query", encryptedOnly: true},
	{name: "LibreDNS", provider: "LibreDNS", category: "Privacy", ipv4: []string{"116.202.176.26"}, tlsName: "dot.libredns.gr", dohURL: "https://doh.libredns.gr/dns-query", encryptedOnly: true},

	// Resolvers run for one country or region.
	{name: "Canadian-Shield", provider: "Canadian-Shield", category: "Regional", ipv4: []string{"149.112.121.10", "149.112.122.10"}, ipv6: []string{"2620:10a:80bb::10", "2620:10a:80bc::10"}, tlsName: "private.canadianshield.cira.ca", dohURL: "https://private.canadianshield.cira.ca/dns-query"},
	{name: "AliDNS", provider: "AliDNS", category: "Regional", ipv4: []string{"223.5.5.5", "223.6.6.6"}, ipv6: []string{"2400:3200::1", "2400:3200:baba::1"}, tlsName: "dns.alidns.com", dohURL: "https://dns.alidns.com/dns-query", doqName: "dns.alidns.com"},
	{name: "DNSPod", provider: "DNSPod", category: "Regional", ipv4: []string{"119.29.29.29", "119.28.28.28"}, ipv6: []string{"2402:4e00::"}},
	{name: "114DNS", provider: "114DNS", category: "Regional", ipv4: []string{"114.114.114.114", "114.114.115.115"}},
}

var (
	DefaultDomains = []string{
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
