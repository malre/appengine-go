// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package datastore

// TODO: Merge this file into query.go.

import (
	"os"

	"appengine"
)


// Count returns the number of results for the query.
func (q *Query) Count(c appengine.Context) (int, os.Error) {
	if q.err != nil {
		return 0, q.err
	}

	if !q.keysOnly {
		// Duplicate the query, set keysOnly.
		newQ := new(Query)
		*newQ = *q
		newQ.keysOnly = true
		q = newQ
	}

	// TODO: This is inefficient. There's no need to
	// fetch results to do a count.
	i := 0
	for t := q.Run(c); ; {
		_, _, err := t.next()
		if err == Done {
			break
		}
		if err != nil {
			return 0, err
		}
		i++
	}
	return i, nil
}
