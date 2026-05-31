package client

// BaseImageRequest is the base request of the image generation.
type BaseImageRequest struct {
	Prompt   string `json:"prompt"`
	Size     string `json:"size"`
	Seed     int    `json:"seed"`
	Footnote string `json:"footnote"`
}

// ImageRequest is the request of the image generation.Some fixed parameters have been added
type ImageRequest struct {
	BaseImageRequest
	Model string `json:"model"`
}

// ImageResponse is the response of the image generation.
type ImageResponse struct {
	ID      string `json:"id"`      // the id of the request
	Created int64  `json:"created"` // unix timestamp
	Data    []Data `json:"data"`    // the data of the image
}

// Data is the data of the image.
type Data struct {
	URL string `json:"url"` // the url of the image
}
