// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package optbuilder

import (
	"fmt"

	"github.com/cockroachdb/cockroach/pkg/sql/opt/memo"
	"github.com/cockroachdb/cockroach/pkg/sql/pgwire/pgerror"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/types"
)

// buildValuesClause builds a set of memo groups that represent the given values
// clause.
//
// See Builder.buildStmt for a description of the remaining input and
// return values.
func (b *Builder) buildValuesClause(values *tree.ValuesClause, inScope *scope) (outScope *scope) {
	var numCols int
	if len(values.Rows) > 0 {
		numCols = len(values.Rows[0])
	}

	colTypes := make([]types.T, numCols)
	for i := range colTypes {
		colTypes[i] = types.Unknown
	}
	rows := make([]memo.GroupID, 0, len(values.Rows))

	// elems is used to store tuple values, and can be allocated once and reused
	// repeatedly, since InternList will copy values to memo storage.
	elems := make([]memo.GroupID, numCols)

	// We need to save and restore the previous value of the field in
	// semaCtx in case we are recursively called within a subquery
	// context.
	defer b.semaCtx.Properties.Restore(b.semaCtx.Properties)

	// Ensure there are no special functions in the clause.
	b.semaCtx.Properties.Require("VALUES", tree.RejectSpecial)

	for _, tuple := range values.Rows {
		if numCols != len(tuple) {
			panic(builderError{pgerror.NewErrorf(
				pgerror.CodeSyntaxError,
				"VALUES lists must all be the same length, expected %d columns, found %d",
				numCols, len(tuple))})
		}

		for i, expr := range tuple {
			texpr := inScope.resolveType(expr, types.Any)
			typ := texpr.ResolvedType()
			elems[i] = b.buildScalar(texpr, inScope)

			// Verify that types of each tuple match one another.
			if colTypes[i] == types.Unknown {
				colTypes[i] = typ
			} else if typ != types.Unknown && !typ.Equivalent(colTypes[i]) {
				panic(builderError{pgerror.NewErrorf(pgerror.CodeDatatypeMismatchError,
					"VALUES types %s and %s cannot be matched", typ, colTypes[i])})
			}
		}

		rows = append(rows, b.factory.ConstructTuple(
			b.factory.InternList(elems), b.factory.InternType(types.TTuple{Types: colTypes})),
		)
	}

	outScope = inScope.push()
	for i := 0; i < numCols; i++ {
		// The column names for VALUES are column1, column2, etc.
		label := fmt.Sprintf("column%d", i+1)
		b.synthesizeColumn(outScope, label, colTypes[i], nil, 0 /* group */)
	}

	colList := colsToColList(outScope.cols)
	outScope.group = b.factory.ConstructValues(
		b.factory.InternList(rows), b.factory.InternColList(colList),
	)
	return outScope
}
