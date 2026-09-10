package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// RowCheck es una comprobación de la revisión de una fila: algo que se puede
// saber ANTES de aplicar, mirando la base.
//
// No reemplaza la comprobación del apply —la fila puede cambiar entre la
// revisión y el COMMIT— pero contesta lo que quien revisa quiere saber ahora:
// si la fila sigue estando, si el padre al que apunta existe, y cuántas hijas
// se lleva un borrado.
type RowCheck struct {
	// Level es "ok", "warn" o "bad". Warn es algo que va a pasar y conviene
	// saber —una cascada—; bad es algo que va a FALLAR al aplicar.
	Level  string `json:"level"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// RowReview es lo que la pantalla de revisión necesita para una fila: las
// columnas de la tabla en su orden —para mostrar la fila entera y no solo lo
// que cambió— y las comprobaciones.
type RowReview struct {
	Columns []schema.DetailColumn `json:"columns"`
	Checks  []RowCheck            `json:"checks"`
}

// ReviewRow arma la revisión de un cambio de datos pendiente.
//
// Cada comprobación es una consulta parametrizada de conteo: barata, y sobre
// una fila. Se hace de a una porque la pantalla muestra de a una, y una
// revisión de cien filas que dispare quinientas consultas al abrirse sería
// exactamente el costo que la pantalla no quiere pagar por adelantado.
func (s *Session) ReviewRow(ctx context.Context, changeID string) (RowReview, error) {
	sesion, err := s.abierta()
	if err != nil {
		return RowReview{}, err
	}
	var c *change.Change
	for _, cand := range sesion.cambios.List() {
		if cand.ID == changeID {
			cand := cand
			c = &cand
			break
		}
	}
	if c == nil {
		return RowReview{}, fmt.Errorf("no hay ningún cambio pendiente con id %q", changeID)
	}
	if c.Kind() != change.KindData {
		return RowReview{}, fmt.Errorf("%s no es un cambio de datos", c.Type)
	}
	d, err := sesion.db.Detail(ctx, c.Schema, c.Table)
	if err != nil {
		return RowReview{}, fmt.Errorf("leer la estructura de %s: %w", c.Target(), err)
	}
	// Los otros cambios de datos incluidos en el apply. Una comprobación que
	// mirara solo la base diría «no existe el padre» cuando el padre es la
	// fila de arriba en la misma tanda, y «el borrado va a fallar» cuando las
	// hijas se borran dos filas antes. Los cambios de datos van todos en la
	// misma fase, así que su orden de ejecución es el de edición: se recorre
	// List, que tiene también a los excluidos, y no Ordered. Con Ordered, un
	// cambio que está excluido no aparece, el corte nunca llega y «antes»
	// terminaba siendo la tanda entera, incluido lo que correría después.
	var otros []change.Change
	for _, cand := range sesion.cambios.List() {
		if cand.ID == c.ID {
			break
		}
		if !cand.Excluded && cand.Kind() == change.KindData {
			otros = append(otros, cand)
		}
	}
	r := revisor{ctx: ctx, s: sesion, c: *c, d: d, antes: otros}
	return RowReview{Columns: d.Columns, Checks: r.todas()}, nil
}

type revisor struct {
	ctx context.Context
	s   *openSession
	c   change.Change
	d   *schema.TableDetail
	// antes son los cambios de datos que corren ANTES que este en el apply.
	antes []change.Change
	out   []RowCheck
}

// pendienteInserta dice si algún cambio anterior de la tanda inserta en la
// tabla una fila con esos valores, o le pone esos valores a una fila.
func (r *revisor) pendienteInserta(esquema, tabla string, where []change.Cell) bool {
	for _, o := range r.antes {
		if !mismoEsquema(o.Schema, esquema) || o.Table != tabla || (o.Type != change.InsertRow && o.Type != change.UpdateRow) {
			continue
		}
		if coincide(where, o.Values) {
			return true
		}
	}
	return false
}

// pendientesBorradas cuenta las filas de la tabla que un cambio anterior de
// la tanda borra y que coinciden con la condición. Se mira Previous, que en
// un borrado es la fila entera.
func (r *revisor) pendientesBorradas(esquema, tabla string, where []change.Cell) int64 {
	var n int64
	for _, o := range r.antes {
		if !mismoEsquema(o.Schema, esquema) || o.Table != tabla || o.Type != change.DeleteRow {
			continue
		}
		if coincide(where, o.Previous) {
			n++
		}
	}
	return n
}

// coincide dice si `fila` tiene cada columna de `where` con el mismo valor.
func coincide(where, fila []change.Cell) bool {
	for _, w := range where {
		i, ok := indiceDe(fila, w.Column)
		if !ok {
			return false
		}
		a, b := w.Value, fila[i].Value
		if (a == nil) != (b == nil) || (a != nil && *a != *b) {
			return false
		}
	}
	return len(where) > 0
}

func (r *revisor) poner(level, label, detail string) {
	r.out = append(r.out, RowCheck{Level: level, Label: label, Detail: detail})
}

func (r *revisor) todas() []RowCheck {
	switch r.c.Type {
	case change.InsertRow:
		r.nulosSinDefault()
		r.padres()
	case change.UpdateRow:
		r.laClaveIdentificaUna()
		r.nulosEnNotNull()
		r.sinCambio()
		r.padres()
	case change.DeleteRow:
		r.laClaveIdentificaUna()
		r.hijas()
	}
	return r.out
}

// laClaveIdentificaUna es la misma comprobación que hace el apply, hecha
// ahora, para que la revisión no diga «va a andar» sobre una fila que otro ya
// borró.
func (r *revisor) laClaveIdentificaUna() {
	n, err := r.s.db.CountWhere(r.ctx, r.c.Schema, r.c.Table, r.c.Key)
	clave := celdasComoTexto(r.c.Key)
	switch {
	case err != nil:
		r.poner("warn", "No se pudo comprobar la clave", err.Error())
	case n == 1:
		r.poner("ok", "La clave identifica una sola fila", clave)
	case n == 0:
		r.poner("bad", "La fila ya no está en la base",
			"Otra sesión la borró o le cambió la clave desde que se leyó. Al aplicar se rechaza: "+clave)
	default:
		r.poner("bad", fmt.Sprintf("La clave alcanza %d filas", n),
			"No es única en la base. Al aplicar se rechaza para no pisar la que no era: "+clave)
	}
}

// hijas dice qué le pasa a cada tabla que apunta a esta cuando la fila se va.
func (r *revisor) hijas() {
	for _, fk := range r.d.ReferencedBy {
		where, ok := r.condicionDesde(fk.RefColumns, fk.Columns, r.c.Key)
		if !ok {
			r.poner("warn", fmt.Sprintf("No se pudo contar las hijas en %s", fk.Table),
				"La clave foránea no apunta a la clave primaria, y la revisión solo conoce la clave primaria de la fila.")
			continue
		}
		n, err := r.s.db.CountWhere(r.ctx, fk.Schema, fk.Table, where)
		if err != nil {
			r.poner("warn", fmt.Sprintf("No se pudo contar las hijas en %s", fk.Table), err.Error())
			continue
		}
		// Las que la misma tanda borra antes ya no van a estar.
		if antes := r.pendientesBorradas(fk.Schema, fk.Table, where); antes > 0 {
			n -= antes
			if n <= 0 {
				r.poner("ok", fmt.Sprintf("Sin filas hijas en %s al momento de borrar", fk.Table),
					fmt.Sprintf("%d se %s antes en esta misma tanda · %s", antes, plural(int(antes), "borra", "borran"), fk.Name))
				continue
			}
		}
		if n == 0 {
			r.poner("ok", fmt.Sprintf("Sin filas hijas en %s", fk.Table), fk.Name)
			continue
		}
		filas := fmt.Sprintf("%d %s en %s", n, plural(int(n), "fila hija", "filas hijas"), fk.Table)
		switch fk.OnDelete {
		case schema.Cascade:
			r.poner("warn", filas+" se borran también", "ON DELETE CASCADE · "+fk.Name)
		case schema.SetNull:
			r.poner("warn", filas+" quedan con NULL", "ON DELETE SET NULL · "+fk.Name)
		case schema.SetDefault:
			r.poner("warn", filas+" vuelven a su valor por defecto", "ON DELETE SET DEFAULT · "+fk.Name)
		default:
			r.poner("bad", filas+": el borrado va a fallar",
				"La clave foránea no deja borrar un padre con hijas (ON DELETE "+
					strings.ToUpper(accionOTexto(fk.OnDelete))+") · "+fk.Name)
		}
	}
}

// padres comprueba que exista el padre al que apunta cada clave foránea cuyo
// valor se está escribiendo.
func (r *revisor) padres() {
	nuevos := r.valoresNuevos()
	for _, fk := range r.d.ForeignKeys {
		// Solo si se está escribiendo alguna de sus columnas: una clave
		// foránea que no se toca ya estaba bien o mal antes de esta edición.
		toca := false
		for _, col := range fk.Columns {
			if _, ok := indiceDe(r.c.Values, col); ok {
				toca = true
			}
		}
		if !toca {
			continue
		}
		valores := make([]change.Cell, 0, len(fk.Columns))
		completa, conNulo := true, false
		for _, col := range fk.Columns {
			v, ok := nuevos[col]
			if !ok {
				completa = false
				break
			}
			if v == nil {
				conNulo = true
			}
			valores = append(valores, change.Cell{Column: col, Value: v})
		}
		if !completa {
			r.poner("warn", fmt.Sprintf("No se pudo comprobar la clave foránea a %s", fk.RefTable),
				"Una de sus columnas toma el valor por defecto, que la revisión no conoce · "+fk.Name)
			continue
		}
		if conNulo {
			r.poner("ok", fmt.Sprintf("Clave foránea a %s en NULL", fk.RefTable),
				"Un NULL no apunta a nada, así que no se comprueba · "+fk.Name)
			continue
		}
		where, ok := r.condicionDesde(fk.Columns, fk.RefColumns, valores)
		if !ok {
			continue
		}
		n, err := r.s.db.CountWhere(r.ctx, fk.RefSchema, fk.RefTable, where)
		if err != nil {
			r.poner("warn", fmt.Sprintf("No se pudo comprobar la clave foránea a %s", fk.RefTable), err.Error())
			continue
		}
		if n == 0 && r.pendienteInserta(fk.RefSchema, fk.RefTable, where) {
			r.poner("ok", fmt.Sprintf("%s con %s lo pone esta misma tanda", fk.RefTable, celdasComoTexto(where)),
				"Un cambio anterior lo inserta, y corre antes · "+fk.Name)
		} else if n == 0 {
			r.poner("bad", fmt.Sprintf("No existe %s con %s", fk.RefTable, celdasComoTexto(where)),
				"La clave foránea va a fallar al aplicar · "+fk.Name)
		} else {
			r.poner("ok", fmt.Sprintf("Existe %s con %s", fk.RefTable, celdasComoTexto(where)), fk.Name)
		}
	}
}

// nulosSinDefault es para un alta: cada columna NOT NULL sin valor por defecto
// ni valor propio va a fallar.
func (r *revisor) nulosSinDefault() {
	var faltan []string
	for _, col := range r.d.Columns {
		if col.Nullable || col.Default != "" || col.Identity != "" || col.Generated != "" {
			continue
		}
		i, ok := indiceDe(r.c.Values, col.Name)
		if !ok {
			faltan = append(faltan, col.Name)
			continue
		}
		if r.c.Values[i].Value == nil {
			r.poner("bad", fmt.Sprintf("%s no admite NULL", col.Name), "Se está insertando NULL en una columna NOT NULL.")
		}
	}
	if len(faltan) > 0 {
		r.poner("bad", fmt.Sprintf("%s sin valor: %s", plural(len(faltan), "Columna NOT NULL", "Columnas NOT NULL"),
			strings.Join(faltan, ", ")), "No tienen valor por defecto, así que el alta va a fallar.")
	} else {
		r.poner("ok", "Las columnas NOT NULL tienen valor", "propio o por defecto")
	}
}

// nulosEnNotNull es para una edición: poner NULL donde no se admite.
func (r *revisor) nulosEnNotNull() {
	for _, v := range r.c.Values {
		if v.Value != nil {
			continue
		}
		for _, col := range r.d.Columns {
			if col.Name == v.Column && !col.Nullable {
				r.poner("bad", fmt.Sprintf("%s no admite NULL", col.Name), "Se está escribiendo NULL en una columna NOT NULL.")
			}
		}
	}
}

// sinCambio avisa de un valor «nuevo» igual al leído: no es un error, pero es
// un UPDATE que no cambia nada y conviene saberlo.
func (r *revisor) sinCambio() {
	for _, v := range r.c.Values {
		i, ok := indiceDe(r.c.Previous, v.Column)
		if !ok {
			continue
		}
		antes := r.c.Previous[i].Value
		if (antes == nil && v.Value == nil) || (antes != nil && v.Value != nil && *antes == *v.Value) {
			r.poner("warn", fmt.Sprintf("%s no cambia", v.Column), "El valor nuevo es igual al leído.")
		}
	}
}

// valoresNuevos es la fila como va a quedar: lo leído, pisado por lo editado.
func (r *revisor) valoresNuevos() map[string]*string {
	out := map[string]*string{}
	for _, p := range r.c.Previous {
		out[p.Column] = p.Value
	}
	for _, v := range r.c.Values {
		out[v.Column] = v.Value
	}
	return out
}

// condicionDesde traduce los valores de unas columnas a una condición sobre
// otras, emparejando por posición: las columnas de una clave foránea con las
// que referencia. Falla si algún valor no está.
func (r *revisor) condicionDesde(desde, hacia []string, valores []change.Cell) ([]change.Cell, bool) {
	if len(desde) != len(hacia) || len(desde) == 0 {
		return nil, false
	}
	out := make([]change.Cell, 0, len(desde))
	for i, col := range desde {
		j, ok := indiceDe(valores, col)
		if !ok {
			return nil, false
		}
		out = append(out, change.Cell{Column: hacia[i], Value: valores[j].Value})
	}
	return out, true
}

func indiceDe(cs []change.Cell, col string) (int, bool) {
	for i, c := range cs {
		if c.Column == col {
			return i, true
		}
	}
	return 0, false
}

// celdasComoTexto escribe «id = 7, tipo = NULL», para las etiquetas.
func celdasComoTexto(cs []change.Cell) string {
	partes := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.Value == nil {
			partes = append(partes, c.Column+" = NULL")
		} else {
			partes = append(partes, c.Column+" = "+*c.Value)
		}
	}
	return strings.Join(partes, ", ")
}

func accionOTexto(a schema.ReferenceAction) string {
	if a == "" {
		return "no action"
	}
	return string(a)
}

// mismoEsquema compara esquemas tomando el vacío como «el de la conexión»:
// un cambio de SQLite lleva "" y el catálogo dice "main"; en MySQL el cambio
// lleva "" y la clave foránea el nombre de la base. Son el mismo lugar.
func mismoEsquema(a, b string) bool {
	return a == b || a == "" || b == ""
}
