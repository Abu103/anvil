// Package parser turns a token stream into an Anvil syntax tree. Statements
// use recursive descent; expressions use Pratt binding powers, so precedence is
// one table instead of a stack of near-identical functions.
package parser

import (
	"errors"
	"fmt"
	"strconv"

	"anvil/ast"
	"anvil/lexer"
)

// Binding powers. Zero means "not an infix operator". All binary operators are
// left-associative: the right operand is parsed with the same power as the
// operator, so an equal-power operator on the right is not consumed.
var bindingPower = map[lexer.TokenKind]int{
	lexer.OROR:   1,
	lexer.ANDAND: 2,
	lexer.EQEQ:   3,
	lexer.NEQ:    3,
	lexer.LT:     4,
	lexer.GT:     4,
	lexer.LE:     4,
	lexer.GE:     4,
	lexer.PLUS:   5,
	lexer.MINUS:  5,
	lexer.STAR:   6,
	lexer.SLASH:  6,
}

const unaryBP = 7

// Parse builds the program. It collects several errors where possible and
// joins them, so a file can be corrected in one pass. It never panics.
func Parse(toks []lexer.Token) (*ast.Program, error) {
	p := &parser{toks: toks}
	prog := p.parseProgram()
	if len(p.errs) > 0 {
		return prog, errors.Join(p.errs...)
	}
	return prog, nil
}

type parser struct {
	toks []lexer.Token
	pos  int
	errs []error
}

func (p *parser) peek() lexer.Token {
	if p.pos >= len(p.toks) {
		if len(p.toks) == 0 {
			return lexer.Token{Kind: lexer.EOF, Line: 1, Col: 1}
		}
		last := p.toks[len(p.toks)-1]
		return lexer.Token{Kind: lexer.EOF, Line: last.Line, Col: last.Col}
	}
	return p.toks[p.pos]
}

func (p *parser) peekNext() lexer.Token {
	if p.pos+1 >= len(p.toks) {
		return p.peek()
	}
	return p.toks[p.pos+1]
}

func (p *parser) advance() lexer.Token {
	tok := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return tok
}

func (p *parser) check(k lexer.TokenKind) bool { return p.peek().Kind == k }

func (p *parser) match(ks ...lexer.TokenKind) bool {
	for _, k := range ks {
		if p.check(k) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *parser) errorAt(tok lexer.Token, format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf("parser: line %d, column %d: %s",
		tok.Line, tok.Col, fmt.Sprintf(format, args...)))
}

// expect reports an error but does not consume the wrong token; the caller's
// recover step then skips to a statement boundary.
func (p *parser) expect(k lexer.TokenKind) lexer.Token {
	if p.check(k) {
		return p.advance()
	}
	got := p.peek()
	p.errorAt(got, "expected %s, got %s", k, got.Kind)
	return got
}

// recover moves to the next place a statement can start, guaranteeing that a
// malformed statement cannot trap the parser in a loop.
func (p *parser) recover() {
	for !p.check(lexer.EOF) {
		switch p.peek().Kind {
		case lexer.SEMICOLON, lexer.RBRACE:
			p.advance()
			return
		case lexer.LET, lexer.IF, lexer.WHILE, lexer.RETURN, lexer.PRINT, lexer.FN:
			return
		default:
			p.advance()
		}
	}
}

func (p *parser) parseProgram() *ast.Program {
	prog := &ast.Program{}
	for !p.check(lexer.EOF) {
		before := p.pos
		if p.check(lexer.FN) {
			if f := p.parseFuncDecl(); f != nil {
				prog.Funcs = append(prog.Funcs, f)
			}
		} else if st := p.parseStmt(); st != nil {
			prog.Main = append(prog.Main, st)
		}
		if p.pos == before { // no progress: never spin
			p.advance()
			p.recover()
		}
	}
	return prog
}

func (p *parser) parseType() string {
	tok := p.peek()
	switch tok.Kind {
	case lexer.INT, lexer.BOOL:
		p.advance()
		return tok.Text
	default:
		p.errorAt(tok, "expected int or bool, got %s", tok.Kind)
		return "int"
	}
}

func (p *parser) parseFuncDecl() *ast.FuncDecl {
	fn := p.expect(lexer.FN)
	name := p.expect(lexer.IDENT)
	p.expect(lexer.LPAREN)

	var params []ast.Param
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		before := p.pos
		ptok := p.expect(lexer.IDENT)
		p.expect(lexer.COLON)
		typ := p.parseType()
		params = append(params, ast.Param{At: pos(ptok), Name: ptok.Text, Type: typ})
		if !p.match(lexer.COMMA) {
			break
		}
		if p.pos == before {
			break
		}
	}
	p.expect(lexer.RPAREN)
	p.expect(lexer.ARROW)
	retType := p.parseType()
	body := p.parseBlock()
	return &ast.FuncDecl{
		At: pos(fn), Name: name.Text, Params: params,
		ReturnType: retType, Body: body,
	}
}

func (p *parser) parseBlock() *ast.BlockStmt {
	open := p.expect(lexer.LBRACE)
	block := &ast.BlockStmt{At: pos(open)}
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		before := p.pos
		if st := p.parseStmt(); st != nil {
			block.Stmts = append(block.Stmts, st)
		}
		if p.pos == before {
			p.advance()
			p.recover()
		}
	}
	p.expect(lexer.RBRACE)
	return block
}

func (p *parser) parseStmt() ast.Stmt {
	tok := p.peek()
	switch tok.Kind {
	case lexer.LET:
		return p.parseLet()
	case lexer.IF:
		return p.parseIf()
	case lexer.WHILE:
		return p.parseWhile()
	case lexer.RETURN:
		return p.parseReturn()
	case lexer.PRINT:
		return p.parsePrint()
	case lexer.LBRACE:
		return p.parseBlock()
	case lexer.IDENT:
		if p.peekNext().Kind == lexer.EQUAL {
			return p.parseAssign()
		}
	}

	// Otherwise it is an expression statement (normally a function call).
	expr := p.parseExpr(0)
	p.expect(lexer.SEMICOLON)
	return &ast.ExprStmt{At: pos(tok), Expr: expr}
}

func (p *parser) parseLet() ast.Stmt {
	kw := p.advance()
	name := p.expect(lexer.IDENT)
	p.expect(lexer.COLON)
	typ := p.parseType()
	p.expect(lexer.EQUAL)
	value := p.parseExpr(0)
	p.expect(lexer.SEMICOLON)
	return &ast.LetStmt{At: pos(kw), Name: name.Text, Type: typ, Value: value}
}

func (p *parser) parseAssign() ast.Stmt {
	name := p.advance()
	p.expect(lexer.EQUAL)
	value := p.parseExpr(0)
	p.expect(lexer.SEMICOLON)
	return &ast.AssignStmt{At: pos(name), Name: name.Text, Value: value}
}

func (p *parser) parseIf() ast.Stmt {
	kw := p.advance()
	p.expect(lexer.LPAREN)
	cond := p.parseExpr(0)
	p.expect(lexer.RPAREN)

	var then *ast.BlockStmt
	if p.check(lexer.LBRACE) {
		then = p.parseBlock()
	} else {
		p.errorAt(p.peek(), "expected { after if condition")
		then = &ast.BlockStmt{At: pos(kw)}
	}

	var elsePart ast.Stmt
	if p.match(lexer.ELSE) {
		if p.check(lexer.IF) {
			elsePart = p.parseIf()
		} else if p.check(lexer.LBRACE) {
			elsePart = p.parseBlock()
		} else {
			p.errorAt(p.peek(), "expected { or if after else")
		}
	}
	return &ast.IfStmt{At: pos(kw), Cond: cond, Then: then, Else: elsePart}
}

func (p *parser) parseWhile() ast.Stmt {
	kw := p.advance()
	p.expect(lexer.LPAREN)
	cond := p.parseExpr(0)
	p.expect(lexer.RPAREN)

	var body *ast.BlockStmt
	if p.check(lexer.LBRACE) {
		body = p.parseBlock()
	} else {
		p.errorAt(p.peek(), "expected { after while condition")
		body = &ast.BlockStmt{At: pos(kw)}
	}
	return &ast.WhileStmt{At: pos(kw), Cond: cond, Body: body}
}

func (p *parser) parseReturn() ast.Stmt {
	kw := p.advance()
	value := p.parseExpr(0) // the spec requires a value
	p.expect(lexer.SEMICOLON)
	return &ast.ReturnStmt{At: pos(kw), Value: value}
}

func (p *parser) parsePrint() ast.Stmt {
	kw := p.advance()
	p.expect(lexer.LPAREN)
	value := p.parseExpr(0)
	p.expect(lexer.RPAREN)
	p.expect(lexer.SEMICOLON)
	return &ast.PrintStmt{At: pos(kw), Value: value}
}

// parseExpr is the Pratt core: parse a prefix, then keep taking binary
// operators whose binding power exceeds minBP.
func (p *parser) parseExpr(minBP int) ast.Expr {
	lhs := p.parsePrefix()
	for {
		op := p.peek()
		bp := bindingPower[op.Kind]
		if bp == 0 || bp <= minBP {
			break
		}
		p.advance()
		rhs := p.parseExpr(bp)
		lhs = &ast.BinaryExpr{At: pos(op), Op: op.Text, Left: lhs, Right: rhs}
	}
	return lhs
}

func (p *parser) parsePrefix() ast.Expr {
	tok := p.peek()
	switch tok.Kind {
	case lexer.MINUS, lexer.BANG:
		p.advance()
		return &ast.UnaryExpr{At: pos(tok), Op: tok.Text, Right: p.parseExpr(unaryBP)}
	case lexer.NUMBER:
		p.advance()
		value, err := strconv.ParseInt(tok.Text, 10, 64)
		if err != nil {
			p.errorAt(tok, "invalid integer %s", tok.Text)
			value = 0
		}
		return &ast.IntLit{At: pos(tok), Value: value, Text: tok.Text}
	case lexer.TRUE, lexer.FALSE:
		p.advance()
		return &ast.BoolLit{At: pos(tok), Value: tok.Kind == lexer.TRUE}
	case lexer.IDENT:
		p.advance()
		if p.check(lexer.LPAREN) {
			return p.parseCall(tok)
		}
		return &ast.Ident{At: pos(tok), Name: tok.Text}
	case lexer.LPAREN:
		p.advance()
		inner := p.parseExpr(0)
		p.expect(lexer.RPAREN)
		return inner
	default:
		p.errorAt(tok, "expected expression, got %s", tok.Kind)
		p.advance() // guarantee progress
		return &ast.IntLit{At: pos(tok), Value: 0, Text: "0"}
	}
}

func (p *parser) parseCall(callee lexer.Token) ast.Expr {
	p.expect(lexer.LPAREN)
	var args []ast.Expr
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		before := p.pos
		args = append(args, p.parseExpr(0))
		if !p.match(lexer.COMMA) {
			break
		}
		if p.pos == before {
			break
		}
	}
	p.expect(lexer.RPAREN)
	return &ast.CallExpr{At: pos(callee), Callee: callee.Text, Args: args}
}

func pos(tok lexer.Token) ast.Position {
	return ast.Position{Line: tok.Line, Col: tok.Col}
}
