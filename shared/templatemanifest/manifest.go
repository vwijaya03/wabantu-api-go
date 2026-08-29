package templatemanifest

import "encoding/json"

const SchemaVersion = 1

const (
	KindChatbot    = "chatbot"
	KindStorefront = "storefront"
	KindBundle     = "bundle"
)

var ValidKinds = map[string]bool{
	KindChatbot:    true,
	KindStorefront: true,
	KindBundle:     true,
}

const (
	LayoutBubbleBottomRight = "bubble-bottom-right"
	LayoutBubbleBottomLeft  = "bubble-bottom-left"
	LayoutFullPage          = "full-page"
	LayoutStoreClassic      = "store-classic"
	LayoutStoreGrid         = "store-grid"
)

const (
	AnimationPlayful = "playful"
	AnimationSlide   = "slide"
	AnimationGlass   = "glass"
	AnimationBounce  = "bounce"
	AnimationMinimal = "minimal"
)

// Manifest is the v1 template definition shared by chatbot and storefront runtimes.
type Manifest struct {
	SchemaVersion   int             `json:"schemaVersion"`
	Kind            string          `json:"kind"`
	Name            string          `json:"name,omitempty"`
	Tokens          TokenSet        `json:"tokens"`
	Layout          string          `json:"layout"`
	AnimationPreset string          `json:"animationPreset"`
	Slots           SlotConfig      `json:"slots"`
	Components      []string        `json:"components,omitempty"`
	Assets          AssetConfig     `json:"assets,omitempty"`
	Extra           json.RawMessage `json:"extra,omitempty"`
}

type TokenSet struct {
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary,omitempty"`
	Background string `json:"background,omitempty"`
	Radius     string `json:"radius,omitempty"`
	FontFamily string `json:"fontFamily,omitempty"`
}

type SlotConfig struct {
	Header        HeaderSlot        `json:"header"`
	MessageBubble MessageBubbleSlot `json:"messageBubble"`
	Hero          *HeroSlot         `json:"hero,omitempty"`
	ProductCard   *ProductCardSlot  `json:"productCard,omitempty"`
}

type HeaderSlot struct {
	ShowAvatar bool   `json:"showAvatar"`
	TitleKey   string `json:"titleKey,omitempty"`
}

type MessageBubbleSlot struct {
	Variant  string `json:"variant"`
	ShowTail bool   `json:"showTail"`
}

type HeroSlot struct {
	Style       string `json:"style,omitempty"`
	ShowParallax bool  `json:"showParallax,omitempty"`
}

type ProductCardSlot struct {
	Variant string `json:"variant,omitempty"`
}

type AssetConfig struct {
	LottieURL   string `json:"lottieUrl,omitempty"`
	LogoURL     string `json:"logoUrl,omitempty"`
	PreviewURL  string `json:"previewUrl,omitempty"`
	FontURL     string `json:"fontUrl,omitempty"`
	CustomCSS   string `json:"customCss,omitempty"`
}
