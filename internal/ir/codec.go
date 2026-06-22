package ir

// Sérialisation CANONIQUE et DÉTERMINISTE d'un *CompiledModel (ADR 0006).
//
// Pourquoi pas gob : gob n'est pas canonique (ordre des champs/maps non garanti, drift
// de version), or feelc vend du déterminisme bit-à-bit inter-plateforme. On encode donc
// à la main, en length-prefixed big-endian, avec :
//   - les maps (Inputs, Domains, context) émises en ordre de clé TRIÉ ;
//   - les décimaux via MarshalText (texte EXACT, sans Reduce → aucune perte) ;
//   - un en-tête magic+version explicite.
//
// Hash(cm) = sha256(Encode(cm)) : identité canonique du modèle compilé (≠ hash du source).
// Deux sources distinctes qui compilent vers le même IR ont le même hash (c'est voulu).

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/maxgfr/feelc/internal/decimal"
)

var magic = [4]byte{'F', 'L', 'I', 'R'}

const codecVersion uint16 = 1

// Encode sérialise un modèle compilé en bytes canoniques.
func Encode(cm *CompiledModel) ([]byte, error) {
	e := &encoder{}
	e.buf = append(e.buf, magic[:]...)
	e.putU16(codecVersion)
	e.putModel(cm)
	if e.err != nil {
		return nil, e.err
	}
	return e.buf, nil
}

// Decode reconstruit un modèle compilé depuis des bytes produits par Encode.
func Decode(b []byte) (*CompiledModel, error) {
	d := &decoder{b: b}
	got, ok := d.need(4)
	if !ok || got[0] != magic[0] || got[1] != magic[1] || got[2] != magic[2] || got[3] != magic[3] {
		return nil, fmt.Errorf("ir: magic invalide (blob non-feelc)")
	}
	if v := d.getU16(); v != codecVersion {
		return nil, fmt.Errorf("ir: version de codec %d non supportée (attendu %d)", v, codecVersion)
	}
	cm := d.getModel()
	if d.err != nil {
		return nil, d.err
	}
	return cm, nil
}

// IsEncoded indique si b commence par le magic feelc-IR (donc produit par Encode).
// Permet au CLI de distinguer une source .rules d'un .ir.bin sans se fier à l'extension.
func IsEncoded(b []byte) bool {
	return len(b) >= 4 && b[0] == magic[0] && b[1] == magic[1] && b[2] == magic[2] && b[3] == magic[3]
}

// Hash renvoie le sha256 de l'encodage canonique (identité du modèle compilé).
func Hash(cm *CompiledModel) ([32]byte, error) {
	b, err := Encode(cm)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// --- encodeur ---

type encoder struct {
	buf []byte
	err error
}

func (e *encoder) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

func (e *encoder) putU8(v uint8)  { e.buf = append(e.buf, v) }
func (e *encoder) putBool(b bool) {
	if b {
		e.putU8(1)
	} else {
		e.putU8(0)
	}
}
func (e *encoder) putU16(v uint16) { e.buf = binary.BigEndian.AppendUint16(e.buf, v) }
func (e *encoder) putU32(v uint32) { e.buf = binary.BigEndian.AppendUint32(e.buf, v) }
func (e *encoder) putBytes(b []byte) {
	e.putU32(uint32(len(b)))
	e.buf = append(e.buf, b...)
}
func (e *encoder) putStr(s string) { e.putBytes([]byte(s)) }

func (e *encoder) putModel(cm *CompiledModel) {
	e.putStr(cm.Name)
	// Inputs (map -> clés triées)
	inNames := sortedMapKeys(cm.Inputs)
	e.putU32(uint32(len(inNames)))
	for _, n := range inNames {
		e.putStr(n)
		e.putU8(uint8(cm.Inputs[n]))
	}
	// Domains (map -> clés triées)
	domNames := sortedMapKeys(cm.Domains)
	e.putU32(uint32(len(domNames)))
	for _, n := range domNames {
		e.putStr(n)
		e.putDomain(cm.Domains[n])
	}
	// Decisions (ordre topo conservé)
	e.putU32(uint32(len(cm.Decisions)))
	for i := range cm.Decisions {
		e.putDecision(cm.Decisions[i])
	}
}

func (e *encoder) putDomain(dom Domain) {
	e.putU8(uint8(dom.Kind))
	e.putValue(dom.Lo)
	e.putValue(dom.Hi)
	e.putBool(dom.LoInf)
	e.putBool(dom.HiInf)
	e.putBool(dom.LoOpen)
	e.putBool(dom.HiOpen)
	e.putU32(uint32(len(dom.Enum)))
	for _, v := range dom.Enum {
		e.putValue(v)
	}
}

func (e *encoder) putDecision(d Decision) {
	e.putStr(d.Name)
	e.putU8(uint8(d.Kind))
	e.putU32(uint32(len(d.Deps)))
	for _, dep := range d.Deps {
		e.putStr(dep)
	}
	// Table (présence)
	if d.Table != nil {
		e.putBool(true)
		e.putTable(d.Table)
	} else {
		e.putBool(false)
	}
	// Expr (présence)
	e.putProg(d.Expr)
}

func (e *encoder) putTable(t *DecisionTable) {
	e.putStrSlice(t.Inputs)
	e.putStrSlice(t.Outputs)
	e.putU8(uint8(t.HitPolicy))
	e.putU8(uint8(t.Agg))
	e.putU32(uint32(len(t.Rules)))
	for _, r := range t.Rules {
		e.putU32(uint32(len(r.Conds)))
		for _, c := range r.Conds {
			e.putCellTest(c)
		}
		e.putValueSlice(r.Outputs)
	}
	e.putValueSlice(t.Priority)
	// Default : nil distinct de [] (présence)
	if t.Default != nil {
		e.putBool(true)
		e.putValueSlice(t.Default)
	} else {
		e.putBool(false)
	}
}

func (e *encoder) putCellTest(c CellTest) {
	e.putU8(uint8(c.Op))
	e.putValue(c.A)
	e.putValue(c.B)
	e.putBool(c.AOpen)
	e.putBool(c.BOpen)
	e.putU32(uint32(len(c.Sub)))
	for _, s := range c.Sub {
		e.putCellTest(s)
	}
	e.putProg(c.Prog)
}

func (e *encoder) putProg(p *ExprProgram) {
	if p == nil {
		e.putBool(false)
		return
	}
	e.putBool(true)
	e.putU32(uint32(len(p.Code)))
	for _, in := range p.Code {
		e.putU8(uint8(in.Op))
		e.putU32(in.Arg)
	}
	e.putValueSlice(p.Consts)
	e.putStrSlice(p.Vars)
	e.putU32(uint32(p.MaxStack))
}

func (e *encoder) putValue(v Value) {
	e.putU8(uint8(v.Tag))
	switch v.Tag {
	case TagNumber:
		if v.Num == nil {
			e.putStr("")
			return
		}
		txt, err := v.Num.MarshalText()
		if err != nil {
			e.fail(err)
			return
		}
		e.putBytes(txt)
	case TagString:
		e.putStr(v.Str)
	case TagBool:
		e.putBool(v.Bool)
	case TagContext:
		names := sortedMapKeys(v.Ctx)
		e.putU32(uint32(len(names)))
		for _, n := range names {
			e.putStr(n)
			e.putValue(v.Ctx[n])
		}
	case TagList:
		e.putValueSlice(v.List)
	}
}

func (e *encoder) putStrSlice(xs []string) {
	e.putU32(uint32(len(xs)))
	for _, x := range xs {
		e.putStr(x)
	}
}

func (e *encoder) putValueSlice(xs []Value) {
	e.putU32(uint32(len(xs)))
	for _, x := range xs {
		e.putValue(x)
	}
}

// --- décodeur ---

type decoder struct {
	b   []byte
	pos int
	err error
}

func (d *decoder) need(n int) ([]byte, bool) {
	if d.err != nil {
		return nil, false
	}
	if n < 0 || d.pos+n > len(d.b) {
		d.err = fmt.Errorf("ir: décodage tronqué (besoin %d à l'offset %d, taille %d)", n, d.pos, len(d.b))
		return nil, false
	}
	s := d.b[d.pos : d.pos+n]
	d.pos += n
	return s, true
}

func (d *decoder) getU8() uint8 {
	s, ok := d.need(1)
	if !ok {
		return 0
	}
	return s[0]
}
func (d *decoder) getBool() bool { return d.getU8() == 1 }
func (d *decoder) getU16() uint16 {
	s, ok := d.need(2)
	if !ok {
		return 0
	}
	return binary.BigEndian.Uint16(s)
}
func (d *decoder) getU32() uint32 {
	s, ok := d.need(4)
	if !ok {
		return 0
	}
	return binary.BigEndian.Uint32(s)
}
func (d *decoder) getBytes() []byte {
	n := int(d.getU32())
	s, ok := d.need(n)
	if !ok {
		return nil
	}
	out := make([]byte, n)
	copy(out, s)
	return out
}
func (d *decoder) getStr() string { return string(d.getBytes()) }

func (d *decoder) getModel() *CompiledModel {
	cm := &CompiledModel{Inputs: map[string]Type{}, Domains: map[string]Domain{}}
	cm.Name = d.getStr()
	nin := int(d.getU32())
	for i := 0; i < nin && d.err == nil; i++ {
		name := d.getStr()
		cm.Inputs[name] = Type(d.getU8())
	}
	ndom := int(d.getU32())
	for i := 0; i < ndom && d.err == nil; i++ {
		name := d.getStr()
		cm.Domains[name] = d.getDomain()
	}
	ndec := int(d.getU32())
	for i := 0; i < ndec && d.err == nil; i++ {
		cm.Decisions = append(cm.Decisions, d.getDecision())
	}
	return cm
}

func (d *decoder) getDomain() Domain {
	dom := Domain{Kind: DomainKind(d.getU8())}
	dom.Lo = d.getValue()
	dom.Hi = d.getValue()
	dom.LoInf = d.getBool()
	dom.HiInf = d.getBool()
	dom.LoOpen = d.getBool()
	dom.HiOpen = d.getBool()
	n := int(d.getU32())
	for i := 0; i < n && d.err == nil; i++ {
		dom.Enum = append(dom.Enum, d.getValue())
	}
	return dom
}

func (d *decoder) getDecision() Decision {
	dec := Decision{}
	dec.Name = d.getStr()
	dec.Kind = DecisionKind(d.getU8())
	ndeps := int(d.getU32())
	for i := 0; i < ndeps && d.err == nil; i++ {
		dec.Deps = append(dec.Deps, d.getStr())
	}
	if d.getBool() {
		dec.Table = d.getTable()
	}
	dec.Expr = d.getProg()
	return dec
}

func (d *decoder) getTable() *DecisionTable {
	t := &DecisionTable{}
	t.Inputs = d.getStrSlice()
	t.Outputs = d.getStrSlice()
	t.HitPolicy = HitPolicy(d.getU8())
	t.Agg = Aggregation(d.getU8())
	nr := int(d.getU32())
	for i := 0; i < nr && d.err == nil; i++ {
		var r Rule
		nc := int(d.getU32())
		for j := 0; j < nc && d.err == nil; j++ {
			r.Conds = append(r.Conds, d.getCellTest())
		}
		r.Outputs = d.getValueSlice()
		t.Rules = append(t.Rules, r)
	}
	t.Priority = d.getValueSlice()
	if d.getBool() {
		t.Default = d.getValueSlice()
		if t.Default == nil {
			t.Default = []Value{} // distinguer "présent vide" de "absent"
		}
	}
	return t
}

func (d *decoder) getCellTest() CellTest {
	c := CellTest{Op: Op(d.getU8())}
	c.A = d.getValue()
	c.B = d.getValue()
	c.AOpen = d.getBool()
	c.BOpen = d.getBool()
	n := int(d.getU32())
	for i := 0; i < n && d.err == nil; i++ {
		c.Sub = append(c.Sub, d.getCellTest())
	}
	c.Prog = d.getProg()
	return c
}

func (d *decoder) getProg() *ExprProgram {
	if !d.getBool() {
		return nil
	}
	p := &ExprProgram{}
	nc := int(d.getU32())
	for i := 0; i < nc && d.err == nil; i++ {
		op := Opcode(d.getU8())
		arg := d.getU32()
		p.Code = append(p.Code, Instr{Op: op, Arg: arg})
	}
	p.Consts = d.getValueSlice()
	p.Vars = d.getStrSlice()
	p.MaxStack = int(d.getU32())
	return p
}

func (d *decoder) getValue() Value {
	v := Value{Tag: Tag(d.getU8())}
	switch v.Tag {
	case TagNumber:
		txt := d.getBytes()
		if len(txt) > 0 {
			num, err := decimal.Parse(string(txt))
			if err != nil && d.err == nil {
				d.err = fmt.Errorf("ir: décimal invalide au décodage: %w", err)
			}
			v.Num = num
		}
	case TagString:
		v.Str = d.getStr()
	case TagBool:
		v.Bool = d.getBool()
	case TagContext:
		n := int(d.getU32())
		v.Ctx = make(map[string]Value, n)
		for i := 0; i < n && d.err == nil; i++ {
			name := d.getStr()
			v.Ctx[name] = d.getValue()
		}
	case TagList:
		v.List = d.getValueSlice()
	}
	return v
}

func (d *decoder) getStrSlice() []string {
	n := int(d.getU32())
	if n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	for i := 0; i < n && d.err == nil; i++ {
		out = append(out, d.getStr())
	}
	return out
}

func (d *decoder) getValueSlice() []Value {
	n := int(d.getU32())
	if n == 0 {
		return nil
	}
	out := make([]Value, 0, n)
	for i := 0; i < n && d.err == nil; i++ {
		out = append(out, d.getValue())
	}
	return out
}

// sortedMapKeys renvoie les clés d'une map à clés string, triées (déterminisme).
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
