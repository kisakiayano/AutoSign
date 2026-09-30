package geopicker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 逆地理编码: 高德(可选) -> Photon -> BigDataCloud -> Nominatim。
var geoClient = &http.Client{Timeout: 6 * time.Second}

// geoCache 缓存解析过的坐标, 避免确认时重复请求。
var geoCache sync.Map

func reverseGeocodeCached(lat, lng float64, amapKey string) string {
	k := strconv.FormatFloat(lat, 'f', 6, 64) + "," + strconv.FormatFloat(lng, 'f', 6, 64)
	if v, ok := geoCache.Load(k); ok {
		return v.(string)
	}
	a := reverseGeocode(lat, lng, amapKey)
	geoCache.Store(k, a)
	return a
}

func reverseGeocode(lat, lng float64, amapKey string) string {
	if amapKey != "" {
		if a := amapReverse(lat, lng, amapKey); a != "" {
			return a
		}
	}
	if a := photonReverse(lat, lng); a != "" {
		return a
	}
	if a := bigDataCloudReverse(lat, lng); a != "" {
		return a
	}
	if a := nominatimReverse(lat, lng); a != "" {
		return a
	}
	return ""
}

func httpGetJSON(rawURL string, out any) error {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "geo-picker/1.0 (location picker)")
	resp, err := geoClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func amapReverse(lat, lng float64, key string) string {
	var r struct {
		Status    string `json:"status"`
		Regeocode struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"regeocode"`
	}
	u := fmt.Sprintf("https://restapi.amap.com/v3/geocode/regeo?key=%s&location=%f,%f", key, lng, lat)
	if err := httpGetJSON(u, &r); err != nil {
		return ""
	}
	return r.Regeocode.FormattedAddress
}

func photonReverse(lat, lng float64) string {
	var r struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	u := fmt.Sprintf("https://photon.komoot.io/reverse?lat=%f&lon=%f", lat, lng)
	if err := httpGetJSON(u, &r); err != nil || len(r.Features) == 0 {
		return ""
	}
	p := r.Features[0].Properties
	get := func(k string) string {
		if v, ok := p[k].(string); ok {
			return v
		}
		return ""
	}
	if get("countrycode") == "CN" {
		return joinUnique([]string{get("state"), get("city"), get("district"), get("locality"), get("street"), get("housenumber"), get("name")}, "")
	}
	return joinUnique([]string{get("name"), get("street"), get("city"), get("state"), get("country")}, ", ")
}

func bigDataCloudReverse(lat, lng float64) string {
	var r struct {
		CountryName          string `json:"countryName"`
		CountryCode          string `json:"countryCode"`
		PrincipalSubdivision string `json:"principalSubdivision"`
		City                 string `json:"city"`
		Locality             string `json:"locality"`
	}
	u := fmt.Sprintf("https://api.bigdatacloud.net/data/reverse-geocode-client?latitude=%f&longitude=%f&localityLanguage=zh", lat, lng)
	if err := httpGetJSON(u, &r); err != nil {
		return ""
	}
	if r.CountryCode == "CN" {
		return joinUnique([]string{r.PrincipalSubdivision, r.City, r.Locality}, "")
	}
	return joinUnique([]string{r.Locality, r.City, r.PrincipalSubdivision, r.CountryName}, ", ")
}

func nominatimReverse(lat, lng float64) string {
	var r struct {
		DisplayName string `json:"display_name"`
	}
	u := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=%f&lon=%f&accept-language=zh-CN", lat, lng)
	if err := httpGetJSON(u, &r); err != nil {
		return ""
	}
	return r.DisplayName
}

func joinUnique(parts []string, sep string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, sep)
}
