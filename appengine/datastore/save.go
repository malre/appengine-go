// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package datastore

import (
	"fmt"
	"os"
	"reflect"

	"appengine"
	"goprotobuf.googlecode.com/hg/proto"

	pb "appengine_internal/datastore"
)

const nilKeyErrStr = "nil key"

// valueToProto converts a named value to a newly allocated Property.
// The returned error string is empty on success.
func valueToProto(defaultAppID, name string, v reflect.Value, multiple bool) (p *pb.Property, errStr string) {
	var (
		pv          pb.PropertyValue
		unsupported bool
	)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		pv.Int64Value = proto.Int64(v.Int())
	case reflect.Bool:
		pv.BooleanValue = proto.Bool(v.Bool())
	case reflect.String:
		pv.StringValue = proto.String(v.String())
	case reflect.Float32, reflect.Float64:
		pv.DoubleValue = proto.Float64(v.Float())
	case reflect.Ptr:
		if k, ok := v.Interface().(*Key); ok {
			if k == nil {
				return nil, nilKeyErrStr
			}
			pv.Referencevalue = keyToReferenceValue(defaultAppID, k)
		} else {
			unsupported = true
		}
	case reflect.Slice:
		if b, ok := v.Interface().([]byte); ok {
			pv.StringValue = proto.String(string(b))
		} else {
			// nvToProto should already catch slice values.
			// If we get here, we have a slice of slice values.
			unsupported = true
		}
	default:
		unsupported = true
	}
	if unsupported {
		return nil, "unsupported datastore value type: " + v.Type().String()
	}
	p = &pb.Property{
		Name:     proto.String(name),
		Value:    &pv,
		Multiple: proto.Bool(multiple),
	}
	switch v.Interface().(type) {
	case []byte:
		p.Meaning = pb.NewProperty_Meaning(pb.Property_BLOB)
	case appengine.BlobKey:
		p.Meaning = pb.NewProperty_Meaning(pb.Property_BLOBKEY)
	case Time:
		p.Meaning = pb.NewProperty_Meaning(pb.Property_GD_WHEN)
	}
	return p, ""
}

// addProperty adds propProto to e, as either a Property or a RawProperty of e
// depending on whether or not the property should be indexed.
// In particular, []byte values are raw. All other values are indexed.
func addProperty(e *pb.EntityProto, propProto *pb.Property, propValue reflect.Value) {
	if propValue.Type() == typeOfByteSlice {
		e.RawProperty = append(e.RawProperty, propProto)
	} else {
		e.Property = append(e.Property, propProto)
	}
}

// nameValue holds a string name and a reflect.Value.
type nameValue struct {
	name  string
	value reflect.Value
}

// nvToProto converts a slice of nameValues to a newly allocated EntityProto.
func nvToProto(defaultAppID string, key *Key, typeName string, nv []nameValue) (*pb.EntityProto, os.Error) {
	const errMsg = "datastore: cannot store field named %q from a %q: %s"
	e := &pb.EntityProto{
		Key: keyToProto(defaultAppID, key),
	}
	if key.parent == nil {
		e.EntityGroup = &pb.Path{}
	} else {
		e.EntityGroup = keyToProto(defaultAppID, key.root()).Path
	}
	for _, x := range nv {
		isBlob := x.value.Type() == typeOfByteSlice
		if x.value.Kind() == reflect.Slice && !isBlob {
			// Save each element of the field as a multiple-valued property.
			for j := 0; j < x.value.Len(); j++ {
				elem := x.value.Index(j)
				property, errStr := valueToProto(defaultAppID, x.name, elem, true)
				if errStr == nilKeyErrStr {
					// Skip a nil *Key.
					continue
				}
				if errStr != "" {
					return nil, fmt.Errorf(errMsg, x.name, typeName, errStr)
				}
				addProperty(e, property, elem)
			}
			continue
		}
		// Save the field as a single-valued property.
		property, errStr := valueToProto(defaultAppID, x.name, x.value, false)
		if errStr == nilKeyErrStr {
			// Skip a nil *Key.
			continue
		}
		if errStr != "" {
			return nil, fmt.Errorf(errMsg, x.name, typeName, errStr)
		}
		addProperty(e, property, x.value)
	}
	if len(e.Property) > maxIndexedProperties {
		return nil, fmt.Errorf("datastore: too many indexed properties")
	}
	return e, nil
}

// saveMap converts an entity Map to a newly allocated EntityProto.
func saveMap(defaultAppID string, key *Key, m Map) (*pb.EntityProto, os.Error) {
	nv := make([]nameValue, len(m))
	n := 0
	for k, v := range m {
		nv[n] = nameValue{k, reflect.ValueOf(v)}
		n++
	}
	return nvToProto(defaultAppID, key, "datastore.Map", nv)
}

// saveEntity saves an EntityProto into a Map, PropertyLoadSaver or struct
// pointer.
func saveEntity(defaultAppID string, key *Key, src interface{}) (x *pb.EntityProto, err os.Error) {
	if m, ok := src.(Map); ok {
		return saveMap(defaultAppID, key, m)
	}

	c := make(chan Property, 32)
	donec := make(chan struct{})
	go func() {
		x, err = propertiesToProto(defaultAppID, key, c)
		close(donec)
	}()
	var err1 os.Error
	if e, ok := src.(PropertyLoadSaver); ok {
		err1 = e.Save(c)
	} else {
		err1 = SaveStruct(src, c)
	}
	<-donec
	if err1 != nil {
		return nil, err1
	}
	return x, err
}

func saveStructProperty(c chan<- Property, name string, noIndex, multiple bool, v reflect.Value) os.Error {
	p := Property{
		Name:     name,
		NoIndex:  noIndex,
		Multiple: multiple,
	}
	switch x := v.Interface().(type) {
	case *Key:
		if x == nil {
			return nil
		}
		p.Value = x
	case Time:
		p.Value = x
	case appengine.BlobKey:
		p.Value = x
	case []byte:
		p.NoIndex = true
		p.Value = x
	default:
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			p.Value = v.Int()
		case reflect.Bool:
			p.Value = v.Bool()
		case reflect.String:
			p.Value = v.String()
		case reflect.Float32, reflect.Float64:
			p.Value = v.Float()
		}
	}
	if p.Value == nil {
		return fmt.Errorf("datastore: unsupported struct field type: %v", v.Type())
	}
	c <- p
	return nil
}

func (s structPLS) Save(c chan<- Property) os.Error {
	defer close(c)
	for i, t := range s.codec.byIndex {
		if t.name == "-" {
			continue
		}
		v := s.v.Field(i)
		if !v.IsValid() || !v.CanSet() {
			continue
		}
		// For slice fields that aren't []byte, save each element.
		if v.Kind() == reflect.Slice && v.Type() != typeOfByteSlice {
			for j := 0; j < v.Len(); j++ {
				if err := saveStructProperty(c, t.name, t.noIndex, true, v.Index(j)); err != nil {
					return err
				}
			}
			continue
		}
		// Otherwise, save the field itself.
		if err := saveStructProperty(c, t.name, t.noIndex, false, v); err != nil {
			return err
		}
	}
	return nil
}

func propertiesToProto(defaultAppID string, key *Key, src <-chan Property) (*pb.EntityProto, os.Error) {
	defer func() {
		for _ = range src {
			// Drain the src channel, if we exit early.
		}
	}()
	e := &pb.EntityProto{
		Key: keyToProto(defaultAppID, key),
	}
	if key.parent == nil {
		e.EntityGroup = &pb.Path{}
	} else {
		e.EntityGroup = keyToProto(defaultAppID, key.root()).Path
	}
	prevMultiple := make(map[string]bool)

	for p := range src {
		if pm, ok := prevMultiple[p.Name]; ok {
			if !pm || !p.Multiple {
				return nil, fmt.Errorf("datastore: multiple Properties with Name %q, but Multiple is false", p.Name)
			}
		} else {
			prevMultiple[p.Name] = p.Multiple
		}

		x := &pb.Property{
			Name:     proto.String(p.Name),
			Value:    new(pb.PropertyValue),
			Multiple: proto.Bool(p.Multiple),
		}
		switch v := p.Value.(type) {
		case int64:
			x.Value.Int64Value = proto.Int64(v)
		case bool:
			x.Value.BooleanValue = proto.Bool(v)
		case string:
			x.Value.StringValue = proto.String(v)
		case float64:
			x.Value.DoubleValue = proto.Float64(v)
		case *Key:
			if v == nil {
				continue
			}
			x.Value.Referencevalue = keyToReferenceValue(defaultAppID, v)
		case Time:
			x.Value.Int64Value = proto.Int64(int64(v))
			x.Meaning = pb.NewProperty_Meaning(pb.Property_GD_WHEN)
		case appengine.BlobKey:
			x.Value.StringValue = proto.String(string(v))
			x.Meaning = pb.NewProperty_Meaning(pb.Property_BLOBKEY)
		case []byte:
			x.Value.StringValue = proto.String(string(v))
			x.Meaning = pb.NewProperty_Meaning(pb.Property_BLOB)
			if !p.NoIndex {
				return nil, fmt.Errorf("datastore: cannot index a []byte valued Property with Name %q", p.Name)
			}
		default:
			return nil, fmt.Errorf("datastore: invalid Value type for a Property with Name %q", p.Name)
		}

		if p.NoIndex {
			e.RawProperty = append(e.RawProperty, x)
		} else {
			e.Property = append(e.Property, x)
			if len(e.Property) > maxIndexedProperties {
				return nil, os.NewError("datastore: too many indexed properties")
			}
		}
	}
	return e, nil
}
