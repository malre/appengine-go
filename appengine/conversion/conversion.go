// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
Package conversion implements an interface to the document conversion service.

Example:
	TODO
*/
package conversion

import (
	"errors"
	"fmt"
	"strconv"

	"appengine"
	"appengine_internal"
	"code.google.com/p/goprotobuf/proto"

	conversion_proto "appengine_internal/conversion"
)

// Asset is a single document asset.
type Asset struct {
	Name string // optional
	Data []byte
	Type string // MIME type (optional)
}

// Document represents a collection of assets.
type Document struct {
	Assets []Asset // must have at least one element
}

// Options represents document conversion options.
// Each field is optional.
type Options struct {
	ImageWidth int
	// TODO: FirstPage, LastPage, InputLanguage
}

func (o *Options) toFlags() (map[string]string, error) {
	// TODO: Sanity check values.
	m := make(map[string]string)

	if o.ImageWidth != 0 {
		m["imageWidth"] = strconv.Itoa(o.ImageWidth)
	}

	return m, nil
}

// Convert converts the document to the given MIME type.
// opts may be nil.
func (d *Document) Convert(c appengine.Context, mimeType string, opts *Options) (*Document, error) {
	req := &conversion_proto.ConversionRequest{
		Conversion: []*conversion_proto.ConversionInput{
			&conversion_proto.ConversionInput{
				Input:          &conversion_proto.DocumentInfo{},
				OutputMimeType: &mimeType,
			},
		},
	}
	for _, asset := range d.Assets {
		a := &conversion_proto.AssetInfo{
			Data: asset.Data,
		}
		if asset.Name != "" {
			a.Name = &asset.Name
		}
		if asset.Type != "" {
			a.MimeType = &asset.Type
		}
		req.Conversion[0].Input.Asset = append(req.Conversion[0].Input.Asset, a)
	}
	if opts != nil {
		f, err := opts.toFlags()
		if err != nil {
			return nil, err
		}
		for k, v := range f {
			req.Conversion[0].Flag = append(req.Conversion[0].Flag, &conversion_proto.ConversionInput_AuxData{
				Key:   proto.String(k),
				Value: proto.String(v),
			})
		}
	}
	res := &conversion_proto.ConversionResponse{}
	if err := c.Call("conversion", "Convert", req, res, nil); err != nil {
		return nil, err
	}
	// We only support one conversion at a time, so the following code assumes that.
	if len(res.Result) != 1 {
		return nil, fmt.Errorf("conversion: requested conversion of one doc, but got %d back", len(res.Result))
	}
	if ec := *res.Result[0].ErrorCode; ec != conversion_proto.ConversionServiceError_OK {
		return nil, fmt.Errorf("conversion: operation failed: %v", ec)
	}
	output := res.Result[0].Output
	if output == nil {
		return nil, errors.New("conversion: output is nil")
	}
	doc := &Document{}
	for _, asset := range output.Asset {
		doc.Assets = append(doc.Assets, Asset{
			Name: asset.GetName(),
			Data: asset.Data,
			Type: asset.GetMimeType(),
		})
	}
	return doc, nil
}

func init() {
	appengine_internal.RegisterErrorCodeMap("conversion", conversion_proto.ConversionServiceError_ErrorCode_name)
}
