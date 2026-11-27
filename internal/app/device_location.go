package app

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"net/netip"
	"sync"

	"github.com/oschwald/maxminddb-golang/v2"
)

// A device's place in Settings › Account stops at its country (the
// device-location decision of 2026-11-05). Leafen runs no service to ask, so
// the data ships in the program and is updated with it: DB-IP's IP to Country
// Lite, under CC BY 4.0 (scripts/update-device-countries.mjs). Private and
// unknown addresses have no place.

//go:embed device_countries.mmdb.gz
var deviceCountriesData []byte

// Unpacked at the first look: a server nobody opens the list on keeps none of
// it in memory. Without it every device's place is unknown.
var deviceCountries = sync.OnceValue(func() *maxminddb.Reader {
	unpacked, err := gzip.NewReader(bytes.NewReader(deviceCountriesData))
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(unpacked)
	if err != nil {
		return nil
	}
	reader, err := maxminddb.OpenBytes(data)
	if err != nil {
		return nil
	}
	return reader
})

// Notion names countries as ISO 3166 does ("Korea, Republic of"); these are the
// names the data has otherwise, apart from Taiwan and Kosovo, which it keeps.
var deviceCountryNames = map[string]string{
	"AX": "Åland Islands", "BL": "Saint Barthélemy", "BN": "Brunei Darussalam",
	"BO": "Bolivia, Plurinational State of", "BQ": "Bonaire, Sint Eustatius and Saba",
	"CC": "Cocos (Keeling) Islands", "CD": "Congo, The Democratic Republic of the", "CG": "Congo",
	"CI": "Côte d'Ivoire", "CW": "Curaçao", "FK": "Falkland Islands (Malvinas)",
	"FM": "Micronesia, Federated States of", "IR": "Iran, Islamic Republic of",
	"KP": "Korea, Democratic People's Republic of", "KR": "Korea, Republic of",
	"LA": "Lao People's Democratic Republic", "MD": "Moldova, Republic of", "MF": "Saint Martin (French part)",
	"NL": "Netherlands", "PS": "Palestine, State of", "RE": "Réunion", "RU": "Russian Federation",
	"SH": "Saint Helena, Ascension and Tristan da Cunha", "SX": "Sint Maarten (Dutch part)",
	"SY": "Syrian Arab Republic", "TL": "Timor-Leste", "TR": "Türkiye", "TZ": "Tanzania, United Republic of",
	"VA": "Holy See (Vatican City State)", "VE": "Venezuela, Bolivarian Republic of",
	"VG": "Virgin Islands, British", "VI": "Virgin Islands, U.S.", "VN": "Viet Nam",
}

// deviceCountry names the country of an address, or "" where it has none.
func deviceCountry(ip string) string {
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	reader := deviceCountries()
	if reader == nil {
		return ""
	}
	var record struct {
		Country struct {
			ISOCode string            `maxminddb:"iso_code"`
			Names   map[string]string `maxminddb:"names"`
		} `maxminddb:"country"`
	}
	if err := reader.Lookup(address.Unmap()).Decode(&record); err != nil || record.Country.ISOCode == "" || record.Country.ISOCode == "ZZ" {
		return ""
	}
	if name, ok := deviceCountryNames[record.Country.ISOCode]; ok {
		return name
	}
	return record.Country.Names["en"]
}
