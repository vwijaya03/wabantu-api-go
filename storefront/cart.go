package storefront

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"encore.app/wabantu/auth"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/publictenant"
)

type CartLine struct {
	LineID    string  `json:"lineId"`
	ProductID string  `json:"productId"`
	Name      string  `json:"name"`
	Qty       int     `json:"qty"`
	UnitPrice float64 `json:"unitPrice"`
}

type Cart struct {
	Items     []CartLine `json:"items"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type CartResponse struct {
	CartToken string `json:"cartToken,omitempty"`
	Cart      Cart   `json:"cart"`
}

type CartItemRequest struct {
	ProductID string `json:"productId"`
	Qty       int    `json:"qty"`
}

func cartKey(tenantID, token string) string {
	return fmt.Sprintf("storefront:cart:%s:%s", tenantID, token)
}

func mintCartToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

//encore:api public method=POST path=/api/v1/public/store/:tenantSlug/cart
func CreatePublicCart(ctx context.Context, tenantSlug string) (*CartResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*CartResponse, error) {
		token := mintCartToken()
		cart := Cart{Items: nil, UpdatedAt: time.Now()}
		if err := saveCart(ctx, ref.TenantID, token, cart); err != nil {
			return nil, err
		}
		return &CartResponse{CartToken: token, Cart: cart}, nil
	})
}

//encore:api public method=GET path=/api/v1/public/store/:tenantSlug/cart/:cartToken
func GetPublicCart(ctx context.Context, tenantSlug, cartToken string) (*CartResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*CartResponse, error) {
		cart, err := loadCart(ctx, ref.TenantID, cartToken)
		if err != nil {
			return nil, err
		}
		return &CartResponse{CartToken: cartToken, Cart: cart}, nil
	})
}

//encore:api public method=PUT path=/api/v1/public/store/:tenantSlug/cart/:cartToken/items
func UpsertCartItem(ctx context.Context, tenantSlug, cartToken string, req *CartItemRequest) (*CartResponse, error) {
	return publictenant.RunPublicTenant(ctx, tenantSlug, resolveTenantBySlug, func(ref publictenant.TenantRef) (*CartResponse, error) {
		if req.Qty < 0 {
			return nil, appErrs.BadRequest("qty tidak valid")
		}
		ts, err := openTenant(ctx, ref.TenantSchema)
		if err != nil {
			return nil, err
		}
		var name string
		var price float64
		err = ts.QueryRowContext(ctx, `
			SELECT name, sell_price FROM `+ts.T("business_catalog_item")+`
			WHERE id = $1::uuid AND deleted_at IS NULL AND is_storefront_visible = true`,
			req.ProductID).Scan(&name, &price)
		if err != nil {
			return nil, appErrs.NotFound("produk tidak ditemukan")
		}
		cart, err := loadCart(ctx, ref.TenantID, cartToken)
		if err != nil {
			return nil, err
		}
		found := false
		for i, ln := range cart.Items {
			if ln.ProductID == req.ProductID {
				if req.Qty == 0 {
					cart.Items = append(cart.Items[:i], cart.Items[i+1:]...)
				} else {
					cart.Items[i].Qty = req.Qty
				}
				found = true
				break
			}
		}
		if !found && req.Qty > 0 {
			cart.Items = append(cart.Items, CartLine{
				LineID: mintCartToken(), ProductID: req.ProductID, Name: name, Qty: req.Qty, UnitPrice: price,
			})
		}
		cart.UpdatedAt = time.Now()
		if err := saveCart(ctx, ref.TenantID, cartToken, cart); err != nil {
			return nil, err
		}
		return &CartResponse{CartToken: cartToken, Cart: cart}, nil
	})
}

func loadCart(ctx context.Context, tenantID, token string) (Cart, error) {
	rdb := auth.RedisClient()
	if rdb == nil {
		return Cart{}, appErrs.Unavailable("cart sementara tidak tersedia")
	}
	raw, err := rdb.Get(ctx, cartKey(tenantID, token)).Bytes()
	if err != nil {
		return Cart{Items: []CartLine{}, UpdatedAt: time.Now()}, nil
	}
	var cart Cart
	if err := json.Unmarshal(raw, &cart); err != nil {
		return Cart{}, err
	}
	return cart, nil
}

func saveCart(ctx context.Context, tenantID, token string, cart Cart) error {
	rdb := auth.RedisClient()
	if rdb == nil {
		return appErrs.Unavailable("cart sementara tidak tersedia")
	}
	raw, err := json.Marshal(cart)
	if err != nil {
		return err
	}
	return rdb.Set(ctx, cartKey(tenantID, token), raw, 7*24*time.Hour).Err()
}
