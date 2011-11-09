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

var typeOfByteSlice = reflect.TypeOf([]byte(nil))

// typeMismatchReason returns a string explaining why the property p could not
// be stored in an entity field of type v.Type().
func typeMismatchReason(p Property, v reflect.Value) string {
	entityType := "empty"
	switch p.Value.(type) {
	case int64:
		entityType = "int"
	case bool:
		entityType = "bool"
	case string:
		entityType = "string"
	case float64:
		entityType = "float"
	case *Key:
		entityType = "*datastore.Key"
	case Time:
		entityType = "datastore.Time"
	case appengine.BlobKey:
		entityType = "appengine.BlobKey"
	case []byte:
		entityType = "[]byte"
	}
	return fmt.Sprintf("type mismatch: %s versus %v", entityType, v.Type())
}

func loadProperty(codec *structCodec, structValue reflect.Value, p Property, requireSlice bool) string {
	index, ok := codec.byName[p.Name]
	if !ok {
		return "no such struct field"
	}
	v := structValue.Field(index)
	if !v.IsValid() {
		return "no such struct field"
	}
	if !v.CanSet() {
		return "cannot set struct field"
	}
	var slice reflect.Value
	if v.Kind() == reflect.Slice && v.Type() != typeOfByteSlice {
		slice = v
		v = reflect.New(v.Type().Elem()).Elem()
	} else if requireSlice {
		return "multiple-valued property requires a slice field type"
	}
	switch v.Kind() {
	case reflect.Int64:
		if x, ok := p.Value.(Time); ok {
			v.SetInt(int64(x))
			break
		}
		fallthrough
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		x, ok := p.Value.(int64)
		if !ok {
			return typeMismatchReason(p, v)
		}
		if v.OverflowInt(x) {
			return fmt.Sprintf("value %v overflows struct field of type %v", x, v.Type())
		}
		v.SetInt(x)
	case reflect.Bool:
		x, ok := p.Value.(bool)
		if !ok {
			return typeMismatchReason(p, v)
		}
		v.SetBool(x)
	case reflect.String:
		if x, ok := p.Value.(appengine.BlobKey); ok {
			v.SetString(string(x))
			break
		}
		x, ok := p.Value.(string)
		if !ok {
			return typeMismatchReason(p, v)
		}
		v.SetString(x)
	case reflect.Float32, reflect.Float64:
		x, ok := p.Value.(float64)
		if !ok {
			return typeMismatchReason(p, v)
		}
		if v.OverflowFloat(x) {
			return fmt.Sprintf("value %v overflows struct field of type %v", x, v.Type())
		}
		v.SetFloat(x)
	case reflect.Ptr:
		x, ok := p.Value.(*Key)
		if !ok {
			return typeMismatchReason(p, v)
		}
		if _, ok := v.Interface().(*Key); !ok {
			return typeMismatchReason(p, v)
		}
		v.Set(reflect.ValueOf(x))
	case reflect.Slice:
		x, ok := p.Value.([]byte)
		if !ok {
			return typeMismatchReason(p, v)
		}
		if _, ok := v.Interface().([]byte); !ok {
			return typeMismatchReason(p, v)
		}
		v.Set(reflect.ValueOf(x))
	default:
		return typeMismatchReason(p, v)
	}
	if slice.IsValid() {
		slice.Set(reflect.Append(slice, v))
	}
	return ""
}

// loadMapEntry converts a Property into an entry of an existing Map,
// or into an element of a slice-valued Map entry.
func loadMapEntry(m Map, p *pb.Property) os.Error {
	var (
		result    interface{}
		sliceType reflect.Type
	)
	switch {
	case p.Value.Int64Value != nil:
		if p.Meaning != nil && *p.Meaning == pb.Property_GD_WHEN {
			result = Time(*p.Value.Int64Value)
			sliceType = reflect.TypeOf([]Time(nil))
		} else {
			result = *p.Value.Int64Value
			sliceType = reflect.TypeOf([]int64(nil))
		}
	case p.Value.BooleanValue != nil:
		result = *p.Value.BooleanValue
		sliceType = reflect.TypeOf([]bool(nil))
	case p.Value.StringValue != nil:
		if p.Meaning != nil && *p.Meaning == pb.Property_BLOB {
			result = []byte(*p.Value.StringValue)
			sliceType = reflect.TypeOf([][]byte(nil))
		} else if p.Meaning != nil && *p.Meaning == pb.Property_BLOBKEY {
			result = appengine.BlobKey(*p.Value.StringValue)
			sliceType = reflect.TypeOf([]appengine.BlobKey(nil))
		} else {
			result = *p.Value.StringValue
			sliceType = reflect.TypeOf([]string(nil))
		}
	case p.Value.DoubleValue != nil:
		result = *p.Value.DoubleValue
		sliceType = reflect.TypeOf([]float64(nil))
	case p.Value.Referencevalue != nil:
		key, err := referenceValueToKey(p.Value.Referencevalue)
		if err != nil {
			return err
		}
		result = key
		sliceType = reflect.TypeOf([]*Key(nil))
	default:
		return nil
	}
	name := proto.GetString(p.Name)
	if proto.GetBool(p.Multiple) {
		var s reflect.Value
		if x := m[name]; x != nil {
			s = reflect.ValueOf(x)
		} else {
			s = reflect.MakeSlice(sliceType, 0, 0)
		}
		s = reflect.Append(s, reflect.ValueOf(result))
		m[name] = s.Interface()
	} else {
		m[name] = result
	}
	return nil
}

// loadMap converts an EntityProto into an existing Map.
func loadMap(m Map, e *pb.EntityProto) (err os.Error) {
	for _, p := range e.Property {
		if err1 := loadMapEntry(m, p); err1 != nil {
			err = err1
		}
	}
	for _, p := range e.RawProperty {
		if err1 := loadMapEntry(m, p); err1 != nil {
			err = err1
		}
	}
	return err
}

// loadEntity loads an EntityProto into a Map, PropertyLoadSaver or struct
// pointer.
func loadEntity(dst interface{}, src *pb.EntityProto) (err os.Error) {
	if m, ok := dst.(Map); ok {
		return loadMap(m, src)
	}

	c := make(chan Property, 32)
	errc := make(chan os.Error, 1)
	defer func() {
		if err == nil {
			err = <-errc
		}
	}()
	go protoToProperties(c, errc, src)
	if e, ok := dst.(PropertyLoadSaver); ok {
		return e.Load(c)
	}
	return LoadStruct(dst, c)
}

func (s structPLS) Load(c <-chan Property) os.Error {
	var fieldName, reason string
	for p := range c {
		if errStr := loadProperty(&s.codec, s.v, p, p.Multiple); errStr != "" {
			// We don't return early, as we try to load as many properties as possible.
			// It is valid to load an entity into a struct that cannot fully represent it.
			// That case returns an error, but the caller is free to ignore it.
			fieldName, reason = p.Name, errStr
		}
	}
	if reason != "" {
		return &ErrFieldMismatch{
			StructType: s.v.Type(),
			FieldName:  fieldName,
			Reason:     reason,
		}
	}
	return nil
}

func protoToProperties(dst chan<- Property, errc chan<- os.Error, src *pb.EntityProto) {
	defer close(dst)
	props, rawProps := src.Property, src.RawProperty
	for {
		var (
			x       *pb.Property
			noIndex bool
		)
		if len(props) > 0 {
			x, props = props[0], props[1:]
		} else if len(rawProps) > 0 {
			x, rawProps = rawProps[0], rawProps[1:]
			noIndex = true
		} else {
			break
		}

		var value interface{}
		switch {
		case x.Value.Int64Value != nil:
			if x.Meaning != nil && *x.Meaning == pb.Property_GD_WHEN {
				value = Time(*x.Value.Int64Value)
			} else {
				value = *x.Value.Int64Value
			}
		case x.Value.BooleanValue != nil:
			value = *x.Value.BooleanValue
		case x.Value.StringValue != nil:
			if x.Meaning != nil && *x.Meaning == pb.Property_BLOB {
				value = []byte(*x.Value.StringValue)
			} else if x.Meaning != nil && *x.Meaning == pb.Property_BLOBKEY {
				value = appengine.BlobKey(*x.Value.StringValue)
			} else {
				value = *x.Value.StringValue
			}
		case x.Value.DoubleValue != nil:
			value = *x.Value.DoubleValue
		case x.Value.Referencevalue != nil:
			key, err := referenceValueToKey(x.Value.Referencevalue)
			if err != nil {
				errc <- err
				return
			}
			value = key
		default:
			errc <- os.NewError("datastore: internal error: stored property has no value")
			return
		}
		dst <- Property{
			Name:     proto.GetString(x.Name),
			Value:    value,
			NoIndex:  noIndex,
			Multiple: proto.GetBool(x.Multiple),
		}
	}
	errc <- nil
}
