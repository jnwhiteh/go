// Copyright 2009 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package Printer

import Scanner "scanner"
import AST "ast"


type Printer /* implements AST.Visitor */ struct {
	indent int;
}


func (P *Printer) NewLine(delta int) {
	P.indent += delta;
	if P.indent < 0 {
		panic("negative indent");
	}
	print("\n");
	for i := P.indent; i > 0; i-- {
		print("\t");
	}
}


func (P *Printer) String(s string) {
	print(s);
}


func (P *Printer) Print(x AST.Node) {
	x.Visit(P);
}


func (P *Printer) PrintList(p *AST.List) {
	if p != nil {
		for i := 0; i < p.len(); i++ {
			if i > 0 {
				P.String(", ");
			}
			P.Print(p.at(i));
		}
	}
}


// ----------------------------------------------------------------------------
// Basics

func (P *Printer) DoNil(x *AST.Nil) {
	P.String("?");
	P.NewLine(0);
}


func (P *Printer) DoIdent(x *AST.Ident) {
	P.String(x.val);
}


// ----------------------------------------------------------------------------
// Types

func (P *Printer) DoFunctionType(x *AST.FunctionType) {
	/*
	if x.recv != nil {
		P.DoVarDeclList(x.recv);
	}
	*/
	P.String("(");
	P.PrintList(x.params);
	P.String(") ");
}


// ----------------------------------------------------------------------------
// Declarations

func (P *Printer) DoBlock(x *AST.Block);


//func (P *Printer) DoVarDeclList(x *VarDeclList) {
//}


func (P *Printer) DoFuncDecl(x *AST.FuncDecl) {
	P.String("func ");
	if x.typ.recv != nil {
		P.String("(");
		P.PrintList(x.typ.recv.idents);
		P.String(") ");
	}
	P.DoIdent(x.ident);
	P.DoFunctionType(x.typ);
	if x.body != nil {
		P.DoBlock(x.body);
	} else {
		P.String(";");
	}
	P.NewLine(0);
	P.NewLine(0);
	P.NewLine(0);
}


// ----------------------------------------------------------------------------
// Expressions

func (P *Printer) DoBinary(x *AST.Binary) {
	print("(");
	P.Print(x.x);
	P.String(" " + Scanner.TokenName(x.tok) + " ");
	P.Print(x.y);
	print(")");
}


func (P *Printer) DoUnary(x *AST.Unary) {
	P.String(Scanner.TokenName(x.tok));
	P.Print(x.x);
}


func (P *Printer) DoLiteral(x *AST.Literal) {
	P.String(x.val);
}


func (P *Printer) DoPair(x *AST.Pair) {
	P.Print(x.x);
	P.String(" : ");
	P.Print(x.y);
}


func (P *Printer) DoIndex(x *AST.Index) {
	P.Print(x.x);
	P.String("[");
	P.Print(x.index);
	P.String("]");
}


func (P *Printer) DoCall(x *AST.Call) {
	P.Print(x.fun);
	P.String("(");
	P.PrintList(x.args);
	P.String(")");
}


func (P *Printer) DoSelector(x *AST.Selector) {
	P.Print(x.x);
	P.String(".");
	P.String(x.field);
}


// ----------------------------------------------------------------------------
// Statements

func (P *Printer) DoBlock(x *AST.Block) {
	if x == nil || x.stats == nil {
		P.NewLine(0);
		return;
	}

	P.String("{");
	P.NewLine(1);
	for i := 0; i < x.stats.len(); i++ {
		if i > 0 {
			P.NewLine(0);
		}
		P.Print(x.stats.at(i));
	}
	P.NewLine(-1);
	P.String("}");
}


func (P *Printer) DoExprStat(x *AST.ExprStat) {
	P.Print(x.expr);
	P.String(";");
}


func (P *Printer) DoAssignment(x *AST.Assignment) {
	P.PrintList(x.lhs);
	P.String(" " + Scanner.TokenName(x.tok) + " ");
	P.PrintList(x.rhs);
	P.String(";");
}


func (P *Printer) DoIfStat(x *AST.IfStat) {
	P.String("if ");
	P.Print(x.init);
	P.String("; ");
	P.Print(x.cond);
	P.DoBlock(x.then);
	if x.else_ != nil {
		P.String(" else ");
		P.DoBlock(x.else_);
	}
}


func (P *Printer) DoForStat(x *AST.ForStat) {
	P.String("for ");
	P.DoBlock(x.body);
}


func (P *Printer) DoSwitch(x *AST.Switch) {
	P.String("switch ");
}


func (P *Printer) DoReturn(x *AST.Return) {
	P.String("return ");
	P.PrintList(x.res);
	P.String(";");
}


// ----------------------------------------------------------------------------
// Program

func (P *Printer) DoProgram(x *AST.Program) {
	P.String("package ");
	P.DoIdent(x.ident);
	P.NewLine(0);
	for i := 0; i < x.decls.len(); i++ {
		P.Print(x.decls.at(i));
	}
}


// ----------------------------------------------------------------------------
// Driver

export func Print(x AST.Node) {
	var P Printer;
	(&P).Print(x);
	print("\n");
}

