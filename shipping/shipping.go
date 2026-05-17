package shipping

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

var secrets struct {
	RajaOngkirAPIKey      string
	RajaOngkirAccountType string // "starter" | "basic" | "pro"
}

// ---------- types ----------

type Province struct {
	ID   string `json:"provinceId"`
	Name string `json:"province"`
}

type City struct {
	ID         string `json:"cityId"`
	ProvinceID string `json:"provinceId"`
	Province   string `json:"province"`
	Type       string `json:"type"`
	Name       string `json:"cityName"`
	PostalCode string `json:"postalCode"`
}

type CostService struct {
	Service     string       `json:"service"`
	Description string       `json:"description"`
	Cost        []CostDetail `json:"cost"`
}

type CostDetail struct {
	Value int    `json:"value"`
	ETD   string `json:"etd"`
	Note  string `json:"note"`
}

type CourierResult struct {
	Code  string        `json:"code"`
	Name  string        `json:"name"`
	Costs []CostService `json:"costs"`
}

type ProvincesResponse struct {
	Provinces []Province `json:"provinces"`
}

type CitiesParams struct {
	ProvinceID string `query:"provinceId"`
}

type CitiesResponse struct {
	Cities []City `json:"cities"`
}

type CostParams struct {
	Origin      string `query:"origin"`
	Destination string `query:"destination"`
	Weight      int    `query:"weight"`
	Courier     string `query:"courier"`
}

type CostResponse struct {
	Results []CourierResult `json:"results"`
}

// ---------- RajaOngkir response wrappers ----------

type rajaOngkirResp struct {
	RajaOngkir struct {
		Status struct {
			Code        int    `json:"code"`
			Description string `json:"description"`
		} `json:"status"`
		Results json.RawMessage `json:"results"`
	} `json:"rajaongkir"`
}

// ---------- endpoints ----------

//encore:api auth method=GET path=/shipping/provinces
func Provinces(ctx context.Context) (*ProvincesResponse, error) {
	body, err := roGet("/province", nil)
	if err != nil {
		return nil, err
	}

	var provinces []Province
	if err := json.Unmarshal(body, &provinces); err != nil {
		return nil, appErrs.Internal("failed to parse provinces")
	}
	return &ProvincesResponse{Provinces: provinces}, nil
}

//encore:api auth method=GET path=/shipping/cities
func Cities(ctx context.Context, p *CitiesParams) (*CitiesResponse, error) {
	params := url.Values{}
	if p.ProvinceID != "" {
		params.Set("province", p.ProvinceID)
	}

	body, err := roGet("/city", params)
	if err != nil {
		return nil, err
	}

	var cities []City
	if err := json.Unmarshal(body, &cities); err != nil {
		return nil, appErrs.Internal("failed to parse cities")
	}
	return &CitiesResponse{Cities: cities}, nil
}

//encore:api auth method=GET path=/shipping/cost
func Cost(ctx context.Context, p *CostParams) (*CostResponse, error) {
	if p.Origin == "" || p.Destination == "" || p.Weight <= 0 || p.Courier == "" {
		return nil, appErrs.BadRequest("origin, destination, weight, and courier are required")
	}

	form := url.Values{
		"origin":      {p.Origin},
		"destination": {p.Destination},
		"weight":      {strconv.Itoa(p.Weight)},
		"courier":     {p.Courier},
	}

	body, err := roPost("/cost", form)
	if err != nil {
		return nil, err
	}

	var results []CourierResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, appErrs.Internal("failed to parse shipping costs")
	}
	return &CostResponse{Results: results}, nil
}

// ---------- internal ----------

func baseURL() string {
	acct := secrets.RajaOngkirAccountType
	if acct == "" {
		acct = "starter"
	}
	return fmt.Sprintf("https://api.rajaongkir.com/%s", acct)
}

func roGet(path string, params url.Values) (json.RawMessage, error) {
	u := baseURL() + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, appErrs.Internal("build request: " + err.Error())
	}
	req.Header.Set("key", secrets.RajaOngkirAPIKey)

	return doRequest(req)
}

func roPost(path string, form url.Values) (json.RawMessage, error) {
	encoded := form.Encode()
	req, err := http.NewRequest("POST", baseURL()+path, strings.NewReader(encoded))
	if err != nil {
		return nil, appErrs.Internal("build request: " + err.Error())
	}
	req.Header.Set("key", secrets.RajaOngkirAPIKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return doRequest(req)
}

func doRequest(req *http.Request) (json.RawMessage, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, appErrs.Unavailable("RajaOngkir API unavailable")
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var wrapper rajaOngkirResp
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, appErrs.Internal("parse RajaOngkir response failed")
	}
	if wrapper.RajaOngkir.Status.Code != 200 {
		return nil, appErrs.Internal("RajaOngkir error: " + wrapper.RajaOngkir.Status.Description)
	}
	return wrapper.RajaOngkir.Results, nil
}
