package main

var (
	builtInResolvers = []DNSServer{
		// Major providers
		{Name: "Cloudflare-1", Addr: "1.1.1.1"},
		{Name: "Cloudflare-2", Addr: "1.0.0.1"},
		{Name: "Google-1", Addr: "8.8.8.8"},
		{Name: "Google-2", Addr: "8.8.4.4"},
		{Name: "Quad9-1", Addr: "9.9.9.9"},
		{Name: "Quad9-2", Addr: "149.112.112.112"},
		{Name: "Quad9-ECS-1", Addr: "9.9.9.11"},
		{Name: "Quad9-ECS-2", Addr: "149.112.112.11"},
		{Name: "OpenDNS-1", Addr: "208.67.222.222"},
		{Name: "OpenDNS-2", Addr: "208.67.220.220"},

		// Ad-blocking and filtering
		{Name: "AdGuard-1", Addr: "94.140.14.14"},
		{Name: "AdGuard-2", Addr: "94.140.15.15"},
		{Name: "CleanBrowsing-1", Addr: "185.228.168.9"},
		{Name: "CleanBrowsing-2", Addr: "185.228.169.9"},
		{Name: "NextDNS-1", Addr: "45.90.28.0"},
		{Name: "NextDNS-2", Addr: "45.90.30.0"},
		{Name: "ControlD-1", Addr: "76.76.2.0"},
		{Name: "ControlD-2", Addr: "76.76.10.0"},

		// Privacy-focused
		{Name: "Mullvad-1", Addr: "194.242.2.2"},
		{Name: "Mullvad-2", Addr: "194.242.2.3"},
		{Name: "DNS0-EU-1", Addr: "193.110.81.0"},
		{Name: "DNS0-EU-2", Addr: "185.253.5.0"},
		{Name: "UncensoredDNS-1", Addr: "91.239.100.100"},
		{Name: "UncensoredDNS-2", Addr: "89.233.43.71"},

		// Regional/National
		{Name: "AliDNS-1", Addr: "223.5.5.5"},
		{Name: "AliDNS-2", Addr: "223.6.6.6"},
		{Name: "DNSPod-1", Addr: "119.29.29.29"},
		{Name: "DNSPod-2", Addr: "119.28.28.28"},
		{Name: "Canadian-Shield-1", Addr: "149.112.121.10"},
		{Name: "Canadian-Shield-2", Addr: "149.112.122.10"},

		// Alternative providers
		{Name: "DNS-SB-1", Addr: "185.222.222.222"},
		{Name: "DNS-SB-2", Addr: "45.11.45.11"},
		{Name: "LibreDNS-1", Addr: "116.202.176.26"},
		{Name: "LibreDNS-2", Addr: "116.203.115.192"},
	}

	builtinMajorResolvers = []DNSServer{
		{Name: "Cloudflare-1", Addr: "1.1.1.1"},
		{Name: "Cloudflare-2", Addr: "1.0.0.1"},

		{Name: "Google-1", Addr: "8.8.8.8"},
		{Name: "Google-2", Addr: "8.8.4.4"},

		{Name: "Quad9-1", Addr: "9.9.9.9"},
		{Name: "Quad9-2", Addr: "149.112.112.112"},

		{Name: "Quad9-ECS-1", Addr: "9.9.9.11"},
		{Name: "Quad9-ECS-2", Addr: "149.112.112.11"},

		{Name: "NextDNS-1", Addr: "45.90.28.0"},
		{Name: "NextDNS-2", Addr: "45.90.30.0"},

		{Name: "AdGuard-1", Addr: "94.140.14.14"},
		{Name: "AdGuard-2", Addr: "94.140.15.15"},
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
