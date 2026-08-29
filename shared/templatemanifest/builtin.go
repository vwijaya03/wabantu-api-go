package templatemanifest

// BuiltinChatbotPlayful is the default platform chatbot template.
func BuiltinChatbotPlayful() Manifest {
	return Manifest{
		SchemaVersion:   SchemaVersion,
		Kind:            KindChatbot,
		Name:            "Playful Chat",
		Layout:          LayoutBubbleBottomRight,
		AnimationPreset: AnimationPlayful,
		Tokens: TokenSet{
			Primary:    "#10b981",
			Secondary:  "#059669",
			Background: "#ffffff",
			Radius:     "1rem",
			FontFamily: "Inter, system-ui, sans-serif",
		},
		Slots: SlotConfig{
			Header: HeaderSlot{ShowAvatar: true, TitleKey: "businessName"},
			MessageBubble: MessageBubbleSlot{
				Variant:  "glass",
				ShowTail: true,
			},
		},
		Components: []string{"QuickReplies", "ProductCarousel"},
	}
}

// BuiltinChatbotGlass is an alternate chatbot template.
func BuiltinChatbotGlass() Manifest {
	m := BuiltinChatbotPlayful()
	m.Name = "Glass Chat"
	m.AnimationPreset = AnimationGlass
	m.Slots.MessageBubble.Variant = "glass"
	m.Tokens.Primary = "#6366f1"
	m.Tokens.Secondary = "#4f46e5"
	return m
}

// BuiltinStorefrontModern is the default platform storefront template.
func BuiltinStorefrontModern() Manifest {
	return Manifest{
		SchemaVersion:   SchemaVersion,
		Kind:            KindStorefront,
		Name:            "Modern Store",
		Layout:          LayoutStoreGrid,
		AnimationPreset: AnimationSlide,
		Tokens: TokenSet{
			Primary:    "#10b981",
			Secondary:  "#047857",
			Background: "#fafafa",
			Radius:     "0.75rem",
			FontFamily: "Inter, system-ui, sans-serif",
		},
		Slots: SlotConfig{
			Header:        HeaderSlot{ShowAvatar: false, TitleKey: "storeTitle"},
			MessageBubble: MessageBubbleSlot{Variant: "solid", ShowTail: false},
			Hero:          &HeroSlot{Style: "gradient", ShowParallax: true},
			ProductCard:   &ProductCardSlot{Variant: "elevated"},
		},
		Components: []string{"ProductGrid", "CartDrawer", "CheckoutSteps"},
	}
}

// BuiltinStorefrontClassic is an alternate storefront template.
func BuiltinStorefrontClassic() Manifest {
	m := BuiltinStorefrontModern()
	m.Name = "Classic Store"
	m.Layout = LayoutStoreClassic
	m.AnimationPreset = AnimationMinimal
	m.Slots.Hero = &HeroSlot{Style: "banner", ShowParallax: false}
	return m
}

// BuiltinBundleEmerald pairs chat + storefront palettes.
func BuiltinBundleEmerald() Manifest {
	return Manifest{
		SchemaVersion:   SchemaVersion,
		Kind:            KindBundle,
		Name:            "Emerald Bundle",
		Layout:          LayoutStoreGrid,
		AnimationPreset: AnimationPlayful,
		Tokens: TokenSet{
			Primary:    "#10b981",
			Secondary:  "#059669",
			Background: "#ffffff",
			Radius:     "1rem",
			FontFamily: "Inter, system-ui, sans-serif",
		},
		Slots: SlotConfig{
			Header:        HeaderSlot{ShowAvatar: true, TitleKey: "businessName"},
			MessageBubble: MessageBubbleSlot{Variant: "glass", ShowTail: true},
			Hero:          &HeroSlot{Style: "gradient", ShowParallax: true},
			ProductCard:   &ProductCardSlot{Variant: "elevated"},
		},
		Components: []string{"QuickReplies", "ProductCarousel", "ProductGrid", "CartDrawer"},
	}
}
