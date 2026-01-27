// Package amap provides a lightweight Go SDK for AMap (Gaode) Web Service APIs.
//
// Design:
// - Client holds auth (key) and HTTP client
// - Sub-services group APIs by domain (Geo, POI2, Place (v3))
// - BaseURL normalization allows callers to configure either:
//   - https://restapi.amap.com
//   - https://restapi.amap.com/v3
//   - https://restapi.amap.com/v5
package amap
