use serde::Serialize;

#[derive(Clone, Copy, Debug, Eq, PartialEq, Ord, PartialOrd, Hash, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ProviderId {
    Cloudflare,
    GitHub,
    GooglePlaces,
    GooglePlay,
    AppleAppStore,
    YouTube,
    Stripe,
    SocialFacebook,
    SocialInstagram,
    SocialX,
    SocialLinkedIn,
    SocialReddit,
    WorkOS,
    AwsIam,
    GcpServiceUsage,
    OpenAI,
    OciCore,
}

impl ProviderId {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Cloudflare => "cloudflare",
            Self::GitHub => "github",
            Self::GooglePlaces => "google_places",
            Self::GooglePlay => "google_play",
            Self::AppleAppStore => "apple_appstore",
            Self::YouTube => "youtube",
            Self::Stripe => "stripe",
            Self::SocialFacebook => "social_facebook",
            Self::SocialInstagram => "social_instagram",
            Self::SocialX => "social_x",
            Self::SocialLinkedIn => "social_linkedin",
            Self::SocialReddit => "social_reddit",
            Self::WorkOS => "workos",
            Self::AwsIam => "aws_iam",
            Self::GcpServiceUsage => "gcp_serviceusage",
            Self::OpenAI => "openai",
            Self::OciCore => "oci_core",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Serialize)]
pub struct ProviderSpec {
    pub base_url: &'static str,
    pub upload_base_url: Option<&'static str>,
    pub api_version: Option<&'static str>,
    pub auth_style: Option<&'static str>,
    pub rate_limit_per_second: f64,
    pub rate_limit_burst: i32,
    pub public_probe: Option<PublicProbe>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
pub struct PublicProbe {
    pub method: &'static str,
    pub path: &'static str,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
pub struct Capability {
    pub supports_pagination: bool,
    pub supports_bulk: bool,
    pub supports_idempotency: bool,
    pub supports_raw: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Serialize)]
pub struct ProviderCatalogEntry {
    pub id: ProviderId,
    pub spec: ProviderSpec,
    pub capabilities: Capability,
}

const ENTRIES: &[ProviderCatalogEntry] = &[
    entry(
        ProviderId::AppleAppStore,
        spec(
            "https://api.appstoreconnect.apple.com",
            None,
            Some("v1"),
            Some("bearer"),
            1.0,
            2,
            Some(PublicProbe {
                method: "GET",
                path: "https://developer.apple.com/sample-code/app-store-connect/app-store-connect-openapi-specification.zip",
            }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::AwsIam,
        spec(
            "https://iam.amazonaws.com",
            None,
            Some("2010-05-08"),
            Some("sigv4"),
            2.0,
            4,
            Some(PublicProbe { method: "GET", path: "/" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::Cloudflare,
        spec(
            "https://api.cloudflare.com/client/v4",
            None,
            Some("v4"),
            None,
            4.0,
            8,
            Some(PublicProbe { method: "GET", path: "/ips" }),
        ),
        caps(true, true, false, true),
    ),
    entry(
        ProviderId::GcpServiceUsage,
        spec(
            "https://serviceusage.googleapis.com",
            None,
            Some("v1"),
            Some("bearer"),
            2.0,
            4,
            Some(PublicProbe {
                method: "GET",
                path: "/v1/services?filter=state:ENABLED&pageSize=1",
            }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::GitHub,
        spec(
            "https://api.github.com",
            None,
            Some("2022-11-28"),
            None,
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/zen" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::GooglePlaces,
        spec(
            "https://places.googleapis.com",
            None,
            Some("v1"),
            None,
            2.0,
            4,
            Some(PublicProbe { method: "GET", path: "/$discovery/rest?version=v1" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::GooglePlay,
        spec(
            "https://androidpublisher.googleapis.com",
            Some("https://androidpublisher.googleapis.com"),
            Some("v3"),
            Some("bearer"),
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/$discovery/rest?version=v3" }),
        ),
        caps(true, true, false, true),
    ),
    entry(
        ProviderId::OciCore,
        spec(
            "https://iaas.us-ashburn-1.oraclecloud.com",
            None,
            Some("20160918"),
            Some("signature"),
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/20160918/instances" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::OpenAI,
        spec(
            "https://api.openai.com",
            None,
            Some("v1"),
            Some("bearer"),
            2.0,
            4,
            Some(PublicProbe { method: "GET", path: "/v1/models?limit=1" }),
        ),
        caps(true, true, true, true),
    ),
    entry(
        ProviderId::SocialFacebook,
        spec(
            "https://graph.facebook.com",
            None,
            Some("v22.0"),
            Some("query"),
            2.0,
            4,
            Some(PublicProbe { method: "GET", path: "/platform" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::SocialInstagram,
        spec(
            "https://graph.facebook.com",
            None,
            Some("v22.0"),
            Some("query"),
            2.0,
            4,
            Some(PublicProbe { method: "GET", path: "/oauth/access_token" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::SocialLinkedIn,
        spec(
            "https://api.linkedin.com",
            None,
            Some("v2"),
            Some("bearer"),
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/v2/me" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::SocialReddit,
        spec(
            "https://oauth.reddit.com",
            None,
            None,
            Some("bearer"),
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/api/v1/scopes" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::SocialX,
        spec(
            "https://api.twitter.com",
            None,
            Some("2"),
            Some("bearer"),
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/2/openapi.json" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::Stripe,
        spec(
            "https://api.stripe.com",
            None,
            Some("account-default"),
            None,
            8.0,
            16,
            Some(PublicProbe { method: "GET", path: "/v1/charges" }),
        ),
        caps(true, true, true, true),
    ),
    entry(
        ProviderId::WorkOS,
        spec(
            "https://api.workos.com",
            None,
            Some("v1"),
            Some("bearer"),
            2.0,
            4,
            Some(PublicProbe { method: "GET", path: "/organizations?limit=1" }),
        ),
        caps(true, false, false, true),
    ),
    entry(
        ProviderId::YouTube,
        spec(
            "https://www.googleapis.com",
            Some("https://www.googleapis.com/upload"),
            Some("v3"),
            None,
            1.0,
            2,
            Some(PublicProbe { method: "GET", path: "/discovery/v1/apis/youtube/v3/rest" }),
        ),
        caps(true, false, false, true),
    ),
];

const fn entry(
    id: ProviderId,
    spec: ProviderSpec,
    capabilities: Capability,
) -> ProviderCatalogEntry {
    ProviderCatalogEntry { id, spec, capabilities }
}

const fn spec(
    base_url: &'static str,
    upload_base_url: Option<&'static str>,
    api_version: Option<&'static str>,
    auth_style: Option<&'static str>,
    rate_limit_per_second: f64,
    rate_limit_burst: i32,
    public_probe: Option<PublicProbe>,
) -> ProviderSpec {
    ProviderSpec {
        base_url,
        upload_base_url,
        api_version,
        auth_style,
        rate_limit_per_second,
        rate_limit_burst,
        public_probe,
    }
}

const fn caps(
    supports_pagination: bool,
    supports_bulk: bool,
    supports_idempotency: bool,
    supports_raw: bool,
) -> Capability {
    Capability { supports_pagination, supports_bulk, supports_idempotency, supports_raw }
}

pub fn default_ids() -> Vec<ProviderId> {
    ENTRIES.iter().map(|entry| entry.id).collect()
}

pub fn find(id: ProviderId) -> Option<&'static ProviderCatalogEntry> {
    ENTRIES.iter().find(|entry| entry.id == id)
}

pub fn parse_id(raw: &str) -> Option<ProviderId> {
    let normalized = raw.trim().to_ascii_lowercase().replace('-', "_");
    match normalized.as_str() {
        "cloudflare" => Some(ProviderId::Cloudflare),
        "github" => Some(ProviderId::GitHub),
        "google_places" | "googleplaces" => Some(ProviderId::GooglePlaces),
        "google_play" | "googleplay" | "play" => Some(ProviderId::GooglePlay),
        "apple" | "appstore" | "app_store" | "apple_appstore" | "appstoreconnect"
        | "app_store_connect" => Some(ProviderId::AppleAppStore),
        "youtube" => Some(ProviderId::YouTube),
        "stripe" => Some(ProviderId::Stripe),
        "social_facebook" | "facebook" => Some(ProviderId::SocialFacebook),
        "social_instagram" | "instagram" => Some(ProviderId::SocialInstagram),
        "social_x" | "x" | "twitter" => Some(ProviderId::SocialX),
        "social_linkedin" | "linkedin" => Some(ProviderId::SocialLinkedIn),
        "social_reddit" | "reddit" => Some(ProviderId::SocialReddit),
        "workos" => Some(ProviderId::WorkOS),
        "aws" | "aws_iam" | "iam" => Some(ProviderId::AwsIam),
        "gcp" | "gcp_serviceusage" | "serviceusage" => Some(ProviderId::GcpServiceUsage),
        "openai" => Some(ProviderId::OpenAI),
        "oci" | "oracle" | "oci_core" => Some(ProviderId::OciCore),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::{ProviderId, default_ids, find, parse_id};

    #[test]
    fn default_catalog_has_expected_count() {
        assert_eq!(default_ids().len(), 17);
    }

    #[test]
    fn parses_aliases() {
        assert_eq!(parse_id("twitter"), Some(ProviderId::SocialX));
        assert_eq!(parse_id("google-play"), Some(ProviderId::GooglePlay));
        assert_eq!(parse_id("app_store_connect"), Some(ProviderId::AppleAppStore));
        assert_eq!(parse_id("iam"), Some(ProviderId::AwsIam));
    }

    #[test]
    fn github_snapshot_matches_expected_values() {
        let github = find(ProviderId::GitHub).expect("github catalog entry");
        assert_eq!(github.spec.base_url, "https://api.github.com");
        assert_eq!(github.spec.api_version, Some("2022-11-28"));
        assert_eq!(github.spec.rate_limit_burst, 2);
        assert!(github.capabilities.supports_raw);
        assert_eq!(github.spec.public_probe.expect("probe").path, "/zen");
    }
}
