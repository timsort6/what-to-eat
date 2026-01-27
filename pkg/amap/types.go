package amap

// openAPIStatus is the common status envelope for AMap WebService APIs.
type openAPIStatus struct {
	Status   string `json:"status"`
	Info     string `json:"info"`
	Infocode string `json:"infocode"`
}

func (s openAPIStatus) ok() bool { return s.Status == "1" }
