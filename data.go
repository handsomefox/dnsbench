package main

var (
	builtInResolvers = []DNSServer{
		// Major providers
		{Name: "Cloudflare", Addr: "1.1.1.1"},
		{Name: "Cloudflare-Alt", Addr: "1.0.0.1"},
		{Name: "Google", Addr: "8.8.8.8"},
		{Name: "Google-Alt", Addr: "8.8.4.4"},
		{Name: "Quad9", Addr: "9.9.9.9"},
		{Name: "Quad9-Alt", Addr: "149.112.112.112"},
		{Name: "OpenDNS", Addr: "208.67.222.222"},
		{Name: "OpenDNS-Alt", Addr: "208.67.220.220"},

		// Ad-blocking and filtering
		{Name: "AdGuard", Addr: "94.140.14.14"},
		{Name: "AdGuard-Alt", Addr: "94.140.15.15"},
		{Name: "CleanBrowsing", Addr: "185.228.168.9"},
		{Name: "CleanBrowsing-Alt", Addr: "185.228.169.9"},
		{Name: "NextDNS", Addr: "45.90.28.0"},
		{Name: "NextDNS-Alt", Addr: "45.90.30.0"},
		{Name: "ControlD", Addr: "76.76.2.0"},
		{Name: "ControlD-Alt", Addr: "76.76.10.0"},

		// Privacy-focused
		{Name: "Mullvad", Addr: "194.242.2.2"},
		{Name: "Mullvad-Alt", Addr: "194.242.2.3"},
		{Name: "DNS0-EU", Addr: "193.110.81.0"},
		{Name: "DNS0-EU-Alt", Addr: "185.253.5.0"},
		{Name: "UncensoredDNS", Addr: "91.239.100.100"},
		{Name: "UncensoredDNS-Alt", Addr: "89.233.43.71"},

		// Regional/National
		{Name: "AliDNS", Addr: "223.5.5.5"},
		{Name: "AliDNS-Alt", Addr: "223.6.6.6"},
		{Name: "DNSPod", Addr: "119.29.29.29"},
		{Name: "DNSPod-Alt", Addr: "119.28.28.28"},
		{Name: "Canadian-Shield", Addr: "149.112.121.10"},
		{Name: "Canadian-Shield-Alt", Addr: "149.112.122.10"},

		// Alternative providers
		{Name: "DNS-SB", Addr: "185.222.222.222"},
		{Name: "DNS-SB-Alt", Addr: "45.11.45.11"},
		{Name: "LibreDNS", Addr: "116.202.176.26"},
		{Name: "LibreDNS-Alt", Addr: "116.203.115.192"},
	}

	builtinMajorResolvers = []DNSServer{
		{Name: "Cloudflare", Addr: "1.1.1.1"},
		{Name: "Cloudflare-Alt", Addr: "1.0.0.1"},
		{Name: "Google", Addr: "8.8.8.8"},
		{Name: "Google-Alt", Addr: "8.8.4.4"},
		{Name: "Quad9", Addr: "9.9.9.9"},
		{Name: "Quad9-Alt", Addr: "149.112.112.112"},
		{Name: "NextDNS", Addr: "45.90.28.0"},
		{Name: "NextDNS-Alt", Addr: "45.90.30.0"},
		{Name: "AdGuard", Addr: "94.140.14.14"},
		{Name: "AdGuard-Alt", Addr: "94.140.15.15"},
	}

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
