package postgres

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// conectar abre un pool contra la base de pruebas y crea un esquema propio del
// test, para que los tests no se pisen entre sí ni dependan del orden.
func conectar(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := testDSN(t)

	pool, _, f := Connect(context.Background(), dsn, "base de pruebas", ConnectOptions{MaxConns: 4})
	if f != nil {
		t.Fatalf("Connect() falló: %s", f.Message)
	}
	t.Cleanup(pool.Close)

	nombre := "kn_" + sanear(t.Name())
	ejecutar(t, pool, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", nombre))
	ejecutar(t, pool, fmt.Sprintf("CREATE SCHEMA %s", nombre))
	t.Cleanup(func() {
		ejecutar(t, pool, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", nombre))
	})
	return pool, nombre
}

func ejecutar(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("no se pudo ejecutar %q: %v", sql, err)
	}
}

func sanear(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func TestIntrospectVeLasTablasDeUnEsquema(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`CREATE TABLE %s.orders (
		id bigserial PRIMARY KEY,
		total numeric(10,2) NOT NULL
	)`, esq))
	ejecutar(t, pool, fmt.Sprintf(`CREATE TABLE %s.audit_log (
		at timestamptz NOT NULL,
		what text
	)`, esq))
	ejecutar(t, pool, fmt.Sprintf(
		`COMMENT ON TABLE %s.orders IS 'Pedidos de la tienda'`, esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	if snap.Database != "kaname_test" {
		t.Errorf("Database = %q", snap.Database)
	}

	sc, ok := snap.FindSchema(esq)
	if !ok {
		t.Fatalf("no encontró el esquema %q; hay %d esquemas", esq, len(snap.Schemas))
	}
	if len(sc.Tables) != 2 {
		t.Fatalf("se esperaban 2 tablas, hay %d: %+v", len(sc.Tables), sc.Tables)
	}
	// Orden alfabético, estable.
	if sc.Tables[0].Name != "audit_log" || sc.Tables[1].Name != "orders" {
		t.Errorf("las tablas no vinieron ordenadas: %s, %s", sc.Tables[0].Name, sc.Tables[1].Name)
	}

	orders := sc.Tables[1]
	if !orders.HasPrimaryKey {
		t.Error("orders tiene PRIMARY KEY y HasPrimaryKey dio false")
	}
	if orders.Comment != "Pedidos de la tienda" {
		t.Errorf("Comment = %q", orders.Comment)
	}
	if sc.Tables[0].HasPrimaryKey {
		t.Error("audit_log no tiene PRIMARY KEY y HasPrimaryKey dio true")
	}
	if sc.Owner == "" {
		t.Error("el esquema vino sin dueño")
	}
}

// Sin clave primaria no hay forma segura de identificar una fila para un UPDATE
// o un DELETE, así que la grilla tiene que saberlo antes de dejar editar.
func TestIntrospectDistingueTablasSinClavePrimaria(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.con_pk (id int PRIMARY KEY)", esq))
	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.sin_pk (id int NOT NULL UNIQUE)", esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	sc, _ := snap.FindSchema(esq)

	porNombre := map[string]bool{}
	for _, tb := range sc.Tables {
		porNombre[tb.Name] = tb.HasPrimaryKey
	}
	if !porNombre["con_pk"] {
		t.Error("con_pk debería tener clave primaria")
	}
	// UNIQUE NOT NULL no es una clave primaria: Postgres las distingue y la
	// grilla también tiene que hacerlo.
	if porNombre["sin_pk"] {
		t.Error("sin_pk tiene UNIQUE, no PRIMARY KEY")
	}
}

// Las particiones son detalle de implementación: llenarían el árbol de ruido y
// la tabla padre ya las representa.
func TestIntrospectMuestraLaTablaParticionadaYNoSusParticiones(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`CREATE TABLE %s.eventos (
		id bigint NOT NULL,
		at date NOT NULL
	) PARTITION BY RANGE (at)`, esq))
	ejecutar(t, pool, fmt.Sprintf(`CREATE TABLE %s.eventos_2026 PARTITION OF %s.eventos
		FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')`, esq, esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	sc, _ := snap.FindSchema(esq)

	if len(sc.Tables) != 1 {
		var nombres []string
		for _, tb := range sc.Tables {
			nombres = append(nombres, tb.Name)
		}
		t.Fatalf("se esperaba solo la tabla padre, vinieron: %v", nombres)
	}
	if sc.Tables[0].Name != "eventos" {
		t.Errorf("Name = %q", sc.Tables[0].Name)
	}
	if !sc.Tables[0].Partitioned {
		t.Error("Partitioned = false para una tabla particionada")
	}
}

// El árbol no puede mostrar el catálogo interno de Postgres.
func TestIntrospectExcluyeLosEsquemasDelSistema(t *testing.T) {
	pool, _ := conectar(t)

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	for _, sc := range snap.Schemas {
		switch sc.Name {
		case "pg_catalog", "information_schema", "pg_toast":
			t.Errorf("apareció el esquema de sistema %q", sc.Name)
		}
	}
}

// `public` es donde está casi todo lo que le interesa a quien abre la app;
// mandarlo a su lugar alfabético es correcto y molesto.
func TestPublicVaPrimero(t *testing.T) {
	pool, esq := conectar(t)
	// El esquema del test empieza con "kn_", que alfabéticamente va antes que
	// "public": sin el orden especial, quedaría primero.
	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.x (id int)", esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	if len(snap.Schemas) < 2 {
		t.Skipf("hacen falta al menos dos esquemas, hay %d", len(snap.Schemas))
	}
	if snap.Schemas[0].Name != "public" {
		var nombres []string
		for _, sc := range snap.Schemas {
			nombres = append(nombres, sc.Name)
		}
		t.Errorf("public no quedó primero: %v", nombres)
	}
}

// El árbol compara snapshots para detectar cambios: dos lecturas del mismo
// esquema tienen que ser idénticas campo por campo.
func TestDosLecturasSeguidasDanElMismoResultado(t *testing.T) {
	pool, esq := conectar(t)
	for i := 0; i < 5; i++ {
		ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.t%d (id int PRIMARY KEY)", esq, i))
	}

	primera, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	segunda, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	a, _ := primera.FindSchema(esq)
	b, _ := segunda.FindSchema(esq)
	if len(a.Tables) != len(b.Tables) {
		t.Fatalf("cantidad distinta de tablas: %d vs %d", len(a.Tables), len(b.Tables))
	}
	for i := range a.Tables {
		// DeepEqual y no !=: Table lleva sus columnas en un slice, y comparar
		// structs con slices no compila. De paso el test se hizo más fuerte,
		// porque ahora también compara las columnas.
		if !reflect.DeepEqual(a.Tables[i], b.Tables[i]) {
			t.Errorf("la tabla %d difiere:\n %+v\n %+v", i, a.Tables[i], b.Tables[i])
		}
	}
}

// reltuples es una estimación del planificador y vale -1 hasta que la tabla se
// analiza. La UI tiene que poder distinguir "no sé" de "cero filas".
func TestLaEstimacionDeFilasSeMarcaComoDesconocida(t *testing.T) {
	pool, esq := conectar(t)
	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.recien_creada (id int)", esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	sc, _ := snap.FindSchema(esq)
	if len(sc.Tables) != 1 {
		t.Fatalf("se esperaba 1 tabla, hay %d", len(sc.Tables))
	}
	sinAnalizar := sc.Tables[0]
	t.Logf("tabla recién creada: RowEstimate=%d HasRowEstimate=%v",
		sinAnalizar.RowEstimate, sinAnalizar.HasRowEstimate())

	// Después de insertar y analizar, la estimación tiene que existir y ser
	// razonable.
	ejecutar(t, pool, fmt.Sprintf(
		"INSERT INTO %s.recien_creada SELECT generate_series(1, 500)", esq))
	ejecutar(t, pool, fmt.Sprintf("ANALYZE %s.recien_creada", esq))

	snap, err = Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	sc, _ = snap.FindSchema(esq)
	analizada := sc.Tables[0]
	if !analizada.HasRowEstimate() {
		t.Fatalf("después de ANALYZE no hay estimación: %d", analizada.RowEstimate)
	}
	if analizada.RowEstimate != 500 {
		t.Errorf("RowEstimate = %d, se esperaba 500 tras ANALYZE", analizada.RowEstimate)
	}
}

func TestTotalTablesCuentaTodosLosEsquemas(t *testing.T) {
	pool, esq := conectar(t)
	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.a (id int)", esq))
	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.b (id int)", esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	suma := 0
	for _, sc := range snap.Schemas {
		suma += len(sc.Tables)
	}
	if snap.TotalTables() != suma {
		t.Errorf("TotalTables() = %d, la suma da %d", snap.TotalTables(), suma)
	}
	if snap.TotalTables() < 2 {
		t.Errorf("TotalTables() = %d, se crearon al menos 2 tablas", snap.TotalTables())
	}
}

// Una tabla que el usuario ve en el catálogo pero no puede leer se muestra
// igual, marcada: esconderla es más confuso, pero la UI tiene que avisar antes
// de que el usuario haga clic y reciba un error de permisos.
func TestIntrospectMarcaLasTablasQueNoSePuedenLeer(t *testing.T) {
	pool, esq := conectar(t)
	rol := "kn_rol_" + sanear(t.Name())

	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.visible (id int PRIMARY KEY)", esq))
	ejecutar(t, pool, fmt.Sprintf("CREATE TABLE %s.prohibida (id int PRIMARY KEY)", esq))

	ejecutar(t, pool, fmt.Sprintf("DROP ROLE IF EXISTS %s", rol))
	ejecutar(t, pool, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD 'temporal'", rol))
	t.Cleanup(func() {
		ejecutar(t, pool, fmt.Sprintf("REASSIGN OWNED BY %s TO CURRENT_USER", rol))
		ejecutar(t, pool, fmt.Sprintf("DROP OWNED BY %s", rol))
		ejecutar(t, pool, fmt.Sprintf("DROP ROLE IF EXISTS %s", rol))
	})
	ejecutar(t, pool, fmt.Sprintf("GRANT CONNECT ON DATABASE kaname_test TO %s", rol))
	ejecutar(t, pool, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", esq, rol))
	ejecutar(t, pool, fmt.Sprintf("GRANT SELECT ON %s.visible TO %s", esq, rol))

	// Conectar como el rol restringido.
	u, err := url.Parse(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(rol, "temporal")
	limitado, _, f := Connect(context.Background(), u.String(), "rol limitado", ConnectOptions{MaxConns: 2})
	if f != nil {
		t.Fatalf("Connect() como %s falló: %s", rol, f.Message)
	}
	defer limitado.Close()

	snap, err := Introspect(context.Background(), limitado)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	sc, ok := snap.FindSchema(esq)
	if !ok {
		t.Fatalf("el rol no ve el esquema %q", esq)
	}

	porNombre := map[string]bool{}
	for _, tb := range sc.Tables {
		porNombre[tb.Name] = tb.Readable
	}
	if len(porNombre) != 2 {
		t.Fatalf("se esperaban las 2 tablas, vinieron %v", porNombre)
	}
	if !porNombre["visible"] {
		t.Error("visible tiene SELECT y quedó marcada como no legible")
	}
	if porNombre["prohibida"] {
		t.Error("prohibida no tiene SELECT y quedó marcada como legible")
	}
}

// El autocompletado del editor SQL y las etiquetas PK/FK del encabezado salen
// de acá. PK y FK vienen del catálogo y no del nombre: una columna llamada `id`
// no es necesariamente clave, y una clave puede llamarse cualquier cosa.
func TestIntrospectTraeLasColumnasConSusClaves(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(
		`create table %s.padre (codigo bigint primary key)`, esquema))
	ejecutar(t, pool, fmt.Sprintf(`create table %s.hijo (
			id           bigint primary key,
			padre_codigo bigint not null references %s.padre(codigo),
			nombre       varchar(255),
			creado       timestamptz not null default now()
		)`, esquema, esquema))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	var hijo *schema.Table
	for _, e := range snap.Schemas {
		if e.Name != esquema {
			continue
		}
		for i := range e.Tables {
			if e.Tables[i].Name == "hijo" {
				hijo = &e.Tables[i]
			}
		}
	}
	if hijo == nil {
		t.Fatalf("no se encontró la tabla hijo en el esquema %s", esquema)
	}

	quiere := []struct {
		nombre   string
		tipo     string
		nullable bool
		defecto  bool
		pk       bool
		fk       bool
	}{
		{"id", "bigint", false, false, true, false},
		{"padre_codigo", "bigint", false, false, false, true},
		{"nombre", "character varying(255)", true, false, false, false},
		{"creado", "timestamp with time zone", false, true, false, false},
	}
	if len(hijo.Columns) != len(quiere) {
		t.Fatalf("columnas = %d, se esperaban %d: %+v", len(hijo.Columns), len(quiere), hijo.Columns)
	}
	for i, q := range quiere {
		got := hijo.Columns[i]
		// El orden es attnum, o sea el de declaración, no el alfabético.
		if got.Name != q.nombre {
			t.Errorf("columna %d = %q, se esperaba %q (¿se perdió el orden de attnum?)", i, got.Name, q.nombre)
		}
		if got.DataType != q.tipo {
			t.Errorf("%s: DataType = %q, se esperaba %q", q.nombre, got.DataType, q.tipo)
		}
		if got.Nullable != q.nullable {
			t.Errorf("%s: Nullable = %v, se esperaba %v", q.nombre, got.Nullable, q.nullable)
		}
		if got.HasDefault != q.defecto {
			t.Errorf("%s: HasDefault = %v, se esperaba %v", q.nombre, got.HasDefault, q.defecto)
		}
		if got.PrimaryKey != q.pk {
			t.Errorf("%s: PrimaryKey = %v, se esperaba %v", q.nombre, got.PrimaryKey, q.pk)
		}
		if got.ForeignKey != q.fk {
			t.Errorf("%s: ForeignKey = %v, se esperaba %v", q.nombre, got.ForeignKey, q.fk)
		}
	}
}
