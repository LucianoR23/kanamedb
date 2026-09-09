package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// versionMayor devuelve la versión mayor del servidor, para saltear los casos
// que prueban features que no existen en las versiones viejas de la matriz.
func versionMayor(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var num int
	if err := pool.QueryRow(context.Background(),
		"select current_setting('server_version_num')::int").Scan(&num); err != nil {
		t.Fatalf("no se pudo leer la versión del servidor: %v", err)
	}
	return num / 10000
}

// buscarTabla encuentra una tabla del snapshot por esquema y nombre.
func buscarTabla(t *testing.T, snap *schema.Snapshot, esq, tabla string) schema.Table {
	t.Helper()
	s, ok := snap.FindSchema(esq)
	if !ok {
		t.Fatalf("el esquema %q no está en el snapshot", esq)
	}
	for _, x := range s.Tables {
		if x.Name == tabla {
			return x
		}
	}
	t.Fatalf("la tabla %q no está en el esquema %q", tabla, esq)
	return schema.Table{}
}

// El orden de las columnas de una clave compuesta es el de la restricción, no el
// de la tabla, y emparejan posicionalmente con las columnas referenciadas.
//
// La fixture está armada para que las dos cosas se distingan: la clave declara
// (y, x) cuando los attnum son (x=1, y=2), y referencia (b, a) cuando los del
// otro lado son (a=1, b=2). Una implementación que ordenara por attnum daría
// ["x","y"] y ["a","b"], y este test la agarraría.
func TestClavesForaneasCompuestasConservanElOrdenDeLaRestriccion(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.padre (
			a int NOT NULL,
			b int NOT NULL,
			UNIQUE (b, a)
		);
		CREATE TABLE %[1]s.hijo (
			x int NOT NULL,
			y int NOT NULL,
			CONSTRAINT hijo_fk FOREIGN KEY (y, x) REFERENCES %[1]s.padre (b, a)
		)`, esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() falló: %v", err)
	}

	hijo := buscarTabla(t, snap, esq, "hijo")
	if len(hijo.ForeignKeys) != 1 {
		t.Fatalf("hijo tiene %d claves foráneas, se esperaba 1: %+v", len(hijo.ForeignKeys), hijo.ForeignKeys)
	}
	fk := hijo.ForeignKeys[0]

	if fk.Name != "hijo_fk" {
		t.Errorf("Name = %q", fk.Name)
	}
	if quiero := []string{"y", "x"}; !reflect.DeepEqual(fk.Columns, quiero) {
		t.Errorf("Columns = %v, se esperaba %v (el orden es el de la restricción)", fk.Columns, quiero)
	}
	if quiero := []string{"b", "a"}; !reflect.DeepEqual(fk.RefColumns, quiero) {
		t.Errorf("RefColumns = %v, se esperaba %v", fk.RefColumns, quiero)
	}
	if fk.RefSchema != esq || fk.RefTable != "padre" {
		t.Errorf("destino = %s.%s, se esperaba %s.padre", fk.RefSchema, fk.RefTable, esq)
	}

	// La tabla apuntada no declara ninguna clave: la relación se ve desde el
	// lado que la declara, que es de donde sale la flecha.
	padre := buscarTabla(t, snap, esq, "padre")
	if len(padre.ForeignKeys) != 0 {
		t.Errorf("padre no debería declarar claves foráneas: %+v", padre.ForeignKeys)
	}
}

// Las acciones referenciales y el carácter opcional deciden cómo se dibuja la
// arista: punteada si el lado es opcional, en color de aviso si borra en
// cascada.
func TestClavesForaneasTraenAccionesYSiSonOpcionales(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.destino (id bigint PRIMARY KEY);

		-- Obligatoria: la columna no admite nulos.
		CREATE TABLE %[1]s.obligatoria (
			id bigint PRIMARY KEY,
			destino_id bigint NOT NULL
				REFERENCES %[1]s.destino (id) ON DELETE CASCADE ON UPDATE RESTRICT
		);

		-- Opcional: la columna admite nulos, así que la fila puede no tener padre.
		CREATE TABLE %[1]s.opcional (
			id bigint PRIMARY KEY,
			destino_id bigint
				REFERENCES %[1]s.destino (id) ON DELETE SET NULL
				DEFERRABLE INITIALLY DEFERRED
		)`, esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() falló: %v", err)
	}

	obl := buscarTabla(t, snap, esq, "obligatoria").ForeignKeys
	if len(obl) != 1 {
		t.Fatalf("obligatoria tiene %d claves", len(obl))
	}
	if obl[0].OnDelete != schema.Cascade {
		t.Errorf("OnDelete = %q, se esperaba %q", obl[0].OnDelete, schema.Cascade)
	}
	if obl[0].OnUpdate != schema.Restrict {
		t.Errorf("OnUpdate = %q, se esperaba %q", obl[0].OnUpdate, schema.Restrict)
	}
	if obl[0].Optional {
		t.Error("Optional = true con una columna NOT NULL")
	}
	if obl[0].Deferrable != "" {
		t.Errorf("Deferrable = %q, se esperaba vacío", obl[0].Deferrable)
	}

	opc := buscarTabla(t, snap, esq, "opcional").ForeignKeys
	if len(opc) != 1 {
		t.Fatalf("opcional tiene %d claves", len(opc))
	}
	if opc[0].OnDelete != schema.SetNull {
		t.Errorf("OnDelete = %q, se esperaba %q", opc[0].OnDelete, schema.SetNull)
	}
	// Sin ON UPDATE explícito Postgres guarda 'a', que es NO ACTION.
	if opc[0].OnUpdate != schema.NoAction {
		t.Errorf("OnUpdate = %q, se esperaba %q", opc[0].OnUpdate, schema.NoAction)
	}
	if !opc[0].Optional {
		t.Error("Optional = false con una columna que admite nulos")
	}
	if opc[0].Deferrable != "initially deferred" {
		t.Errorf("Deferrable = %q, se esperaba \"initially deferred\"", opc[0].Deferrable)
	}
}

// Una clave compuesta con una sola columna nullable es opcional: con MATCH
// SIMPLE alcanza un NULL para que Postgres no exija la fila padre. Si el cálculo
// pidiera que TODAS admitan nulos, este caso daría obligatoria y la arista se
// dibujaría llena mintiendo sobre la relación.
func TestClaveCompuestaConUnaSolaColumnaNullableEsOpcional(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.padre (a int NOT NULL, b int NOT NULL, UNIQUE (a, b));
		CREATE TABLE %[1]s.hijo (
			a int NOT NULL,
			b int,
			FOREIGN KEY (a, b) REFERENCES %[1]s.padre (a, b)
		)`, esq))

	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() falló: %v", err)
	}
	fks := buscarTabla(t, snap, esq, "hijo").ForeignKeys
	if len(fks) != 1 {
		t.Fatalf("hijo tiene %d claves", len(fks))
	}
	if !fks[0].Optional {
		t.Error("Optional = false: una sola columna nullable ya hace opcional la relación")
	}
}

func TestDetailTraeColumnasConDefaultComentarioEIdentidad(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.cosas (
			id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			nombre text NOT NULL,
			precio numeric(10,2) NOT NULL DEFAULT 0.00,
			total numeric(10,2) GENERATED ALWAYS AS (precio * 2) STORED,
			nota text
		);
		COMMENT ON TABLE %[1]s.cosas IS 'tabla de prueba';
		COMMENT ON COLUMN %[1]s.cosas.precio IS 'sin IVA'`, esq))

	d, err := Detail(context.Background(), pool, esq, "cosas")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}

	if d.Schema != esq || d.Name != "cosas" {
		t.Errorf("identidad = %s.%s", d.Schema, d.Name)
	}
	if d.Comment != "tabla de prueba" {
		t.Errorf("Comment = %q", d.Comment)
	}
	if len(d.Columns) != 5 {
		t.Fatalf("hay %d columnas, se esperaban 5: %+v", len(d.Columns), d.Columns)
	}

	porNombre := map[string]schema.DetailColumn{}
	for _, c := range d.Columns {
		porNombre[c.Name] = c
	}

	if c := porNombre["id"]; c.Identity != "always" || !c.PrimaryKey {
		t.Errorf("id: Identity = %q, PrimaryKey = %v", c.Identity, c.PrimaryKey)
	}
	if c := porNombre["precio"]; c.Comment != "sin IVA" || c.Default == "" {
		t.Errorf("precio: Comment = %q, Default = %q", c.Comment, c.Default)
	}
	if c := porNombre["precio"]; c.DataType != "numeric(10,2)" {
		t.Errorf("precio: DataType = %q, se esperaba numeric(10,2)", c.DataType)
	}
	if c := porNombre["total"]; c.Generated != "stored" {
		t.Errorf("total: Generated = %q, se esperaba \"stored\"", c.Generated)
	}
	if c := porNombre["nota"]; !c.Nullable || c.Default != "" || c.Comment != "" {
		t.Errorf("nota: Nullable = %v, Default = %q, Comment = %q", c.Nullable, c.Default, c.Comment)
	}
	if c := porNombre["nombre"]; c.Nullable {
		t.Error("nombre: Nullable = true en una columna NOT NULL")
	}

	// Las posiciones son las de la tabla y llegan en orden.
	for i, c := range d.Columns {
		if c.Position != i+1 {
			t.Errorf("columna %d (%s): Position = %d", i, c.Name, c.Position)
		}
	}
}

func TestDetailTraeIndicesConSusModificadores(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.eventos (
			id bigint PRIMARY KEY,
			email text NOT NULL,
			creado timestamptz NOT NULL,
			borrado boolean NOT NULL DEFAULT false
		);
		CREATE UNIQUE INDEX eventos_email_uq ON %[1]s.eventos (lower(email));
		CREATE INDEX eventos_creado_idx ON %[1]s.eventos (creado DESC);
		CREATE INDEX eventos_nulos_idx ON %[1]s.eventos (creado NULLS FIRST);
		CREATE INDEX eventos_cubre_idx ON %[1]s.eventos (creado) INCLUDE (email);
		CREATE INDEX eventos_vivos_idx ON %[1]s.eventos (creado) WHERE NOT borrado`, esq))

	d, err := Detail(context.Background(), pool, esq, "eventos")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if len(d.Indexes) != 6 {
		t.Fatalf("hay %d índices, se esperaban 6: %+v", len(d.Indexes), d.Indexes)
	}

	// La clave primaria va primero: es la que se busca al abrir la pestaña.
	if !d.Indexes[0].Primary {
		t.Errorf("el primer índice no es la clave primaria: %+v", d.Indexes[0])
	}

	porNombre := map[string]schema.Index{}
	for _, i := range d.Indexes {
		porNombre[i.Name] = i
	}

	// Un índice funcional: la columna es la expresión, no un nombre de columna.
	// Reconstruirla desde pg_attribute daría algo que no existe en la tabla.
	if ix := porNombre["eventos_email_uq"]; !ix.Unique ||
		len(ix.Columns) != 1 || !strings.Contains(ix.Columns[0], "lower(email)") {
		t.Errorf("eventos_email_uq: Unique = %v, Columns = %v", ix.Unique, ix.Columns)
	}

	// El DESC es parte de cómo está declarado el índice y decide si sirve para
	// un ORDER BY. Perderlo lo haría ver igual que uno ascendente.
	//
	// Un DESC trae NULLS FIRST por default, así que declararlo sin nada no debe
	// agregar ningún NULLS: solo se escribe lo que contradice el default.
	if ix := porNombre["eventos_creado_idx"]; !reflect.DeepEqual(ix.Columns, []string{"creado DESC"}) {
		t.Errorf("eventos_creado_idx: Columns = %v, se esperaba [\"creado DESC\"]", ix.Columns)
	}

	// Ascendente el default es NULLS LAST, así que pedir NULLS FIRST sí se dice.
	if ix := porNombre["eventos_nulos_idx"]; !reflect.DeepEqual(ix.Columns, []string{"creado NULLS FIRST"}) {
		t.Errorf("eventos_nulos_idx: Columns = %v, se esperaba [\"creado NULLS FIRST\"]", ix.Columns)
	}

	// Una columna de INCLUDE no es clave: mezclarla con las claves haría creer
	// que el índice sirve para buscar por ella.
	cubre := porNombre["eventos_cubre_idx"]
	if !reflect.DeepEqual(cubre.Columns, []string{"creado"}) {
		t.Errorf("eventos_cubre_idx: Columns = %v, se esperaba [\"creado\"]", cubre.Columns)
	}
	if !reflect.DeepEqual(cubre.Included, []string{"email"}) {
		t.Errorf("eventos_cubre_idx: Included = %v, se esperaba [\"email\"]", cubre.Included)
	}
	if ix := porNombre["eventos_creado_idx"]; len(ix.Included) != 0 {
		t.Errorf("eventos_creado_idx: Included = %v en un índice sin INCLUDE", ix.Included)
	}

	// Un índice parcial que se ve como total es una mentira cara.
	if ix := porNombre["eventos_vivos_idx"]; ix.Predicate == "" {
		t.Errorf("eventos_vivos_idx: Predicate vacío en un índice parcial")
	}
	if ix := porNombre["eventos_creado_idx"]; ix.Predicate != "" {
		t.Errorf("eventos_creado_idx: Predicate = %q en un índice total", ix.Predicate)
	}

	for _, ix := range d.Indexes {
		if ix.Method != "btree" {
			t.Errorf("%s: Method = %q", ix.Name, ix.Method)
		}
		if ix.SizeBytes <= 0 {
			t.Errorf("%s: SizeBytes = %d", ix.Name, ix.SizeBytes)
		}
		if !strings.HasPrefix(ix.Definition, "CREATE ") {
			t.Errorf("%s: Definition = %q", ix.Name, ix.Definition)
		}
		if !ix.HasScans() {
			t.Errorf("%s: sin estadísticas de uso (Scans = %d)", ix.Name, ix.Scans)
		}
	}
}

func TestDetailDistingueLasRestriccionesSinValidar(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.pedidos (
			id bigint PRIMARY KEY,
			total numeric(10,2) NOT NULL,
			enviado timestamptz,
			puesto timestamptz NOT NULL,
			CONSTRAINT pedidos_total_no_negativo CHECK (total >= 0)
		);
		INSERT INTO %[1]s.pedidos VALUES (1, 10, NULL, now());
		ALTER TABLE %[1]s.pedidos
			ADD CONSTRAINT pedidos_enviado_despues
			CHECK (enviado IS NULL OR enviado >= puesto) NOT VALID`, esq))

	d, err := Detail(context.Background(), pool, esq, "pedidos")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if len(d.Checks) != 2 {
		t.Fatalf("hay %d restricciones, se esperaban 2: %+v", len(d.Checks), d.Checks)
	}

	porNombre := map[string]schema.CheckConstraint{}
	for _, c := range d.Checks {
		porNombre[c.Name] = c
	}

	valida := porNombre["pedidos_total_no_negativo"]
	if !valida.Validated {
		t.Error("pedidos_total_no_negativo: Validated = false")
	}
	if strings.HasPrefix(valida.Expression, "CHECK") {
		t.Errorf("la expresión conserva el envoltorio: %q", valida.Expression)
	}
	if !strings.Contains(valida.Expression, "total") {
		t.Errorf("Expression = %q", valida.Expression)
	}

	// Una restricción sin validar no garantiza lo que dice: las filas que ya
	// estaban nunca se comprobaron.
	if sinValidar := porNombre["pedidos_enviado_despues"]; sinValidar.Validated {
		t.Error("pedidos_enviado_despues: Validated = true en una restricción NOT VALID")
	}
}

func TestDetailTraeLosTriggersDeUsuarioYNoLosInternos(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE FUNCTION %[1]s.tocar() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RETURN NEW; END $$;

		CREATE TABLE %[1]s.destino (id bigint PRIMARY KEY);
		CREATE TABLE %[1]s.auditada (
			id bigint PRIMARY KEY,
			-- Esta clave foránea le hace crear a Postgres triggers internos en
			-- las dos tablas. No los escribió nadie y no deben aparecer.
			destino_id bigint REFERENCES %[1]s.destino (id)
		);

		CREATE TRIGGER auditada_antes BEFORE INSERT OR UPDATE ON %[1]s.auditada
			FOR EACH ROW EXECUTE FUNCTION %[1]s.tocar();
		CREATE TRIGGER auditada_despues AFTER DELETE ON %[1]s.auditada
			FOR EACH STATEMENT EXECUTE FUNCTION %[1]s.tocar();
		ALTER TABLE %[1]s.auditada DISABLE TRIGGER auditada_despues`, esq))

	d, err := Detail(context.Background(), pool, esq, "auditada")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if len(d.Triggers) != 2 {
		t.Fatalf("hay %d triggers, se esperaban 2 (los internos de la FK no cuentan): %+v",
			len(d.Triggers), d.Triggers)
	}

	porNombre := map[string]schema.Trigger{}
	for _, tg := range d.Triggers {
		porNombre[tg.Name] = tg
	}

	antes := porNombre["auditada_antes"]
	if antes.Timing != "before" || antes.Level != "row" {
		t.Errorf("auditada_antes: Timing = %q, Level = %q", antes.Timing, antes.Level)
	}
	if quiero := []string{"insert", "update"}; !reflect.DeepEqual(antes.Events, quiero) {
		t.Errorf("auditada_antes: Events = %v, se esperaba %v", antes.Events, quiero)
	}
	if !antes.Enabled {
		t.Error("auditada_antes: Enabled = false")
	}
	if !strings.HasSuffix(antes.Function, ".tocar()") {
		t.Errorf("auditada_antes: Function = %q, se esperaba calificada con el esquema", antes.Function)
	}

	despues := porNombre["auditada_despues"]
	if despues.Timing != "after" || despues.Level != "statement" {
		t.Errorf("auditada_despues: Timing = %q, Level = %q", despues.Timing, despues.Level)
	}
	// Un trigger deshabilitado que se ve igual que uno activo hace perder tardes.
	if despues.Enabled {
		t.Error("auditada_despues: Enabled = true en un trigger deshabilitado")
	}
}

// Para entender qué pasa al borrar una fila hacen falta las dos direcciones: un
// ON DELETE CASCADE que borra en otra tabla no se ve mirando solo las claves
// propias.
func TestDetailVeLasClavesQueApuntanAEstaTabla(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.clientes (id bigint PRIMARY KEY);
		CREATE TABLE %[1]s.pedidos (
			id bigint PRIMARY KEY,
			cliente_id bigint NOT NULL REFERENCES %[1]s.clientes (id) ON DELETE CASCADE
		)`, esq))

	cli, err := Detail(context.Background(), pool, esq, "clientes")
	if err != nil {
		t.Fatalf("Detail(clientes) falló: %v", err)
	}
	if len(cli.ForeignKeys) != 0 {
		t.Errorf("clientes no declara claves: %+v", cli.ForeignKeys)
	}
	if len(cli.ReferencedBy) != 1 {
		t.Fatalf("clientes tiene %d claves entrantes, se esperaba 1", len(cli.ReferencedBy))
	}
	// En una clave entrante, Columns son las de la tabla que referencia y el
	// destino es esta misma tabla.
	entrante := cli.ReferencedBy[0]
	if quiero := []string{"cliente_id"}; !reflect.DeepEqual(entrante.Columns, quiero) {
		t.Errorf("Columns = %v, se esperaba %v", entrante.Columns, quiero)
	}
	if entrante.RefTable != "clientes" {
		t.Errorf("RefTable = %q, se esperaba clientes", entrante.RefTable)
	}
	if entrante.OnDelete != schema.Cascade {
		t.Errorf("OnDelete = %q", entrante.OnDelete)
	}

	ped, err := Detail(context.Background(), pool, esq, "pedidos")
	if err != nil {
		t.Fatalf("Detail(pedidos) falló: %v", err)
	}
	if len(ped.ForeignKeys) != 1 {
		t.Errorf("pedidos declara %d claves, se esperaba 1", len(ped.ForeignKeys))
	}
	if len(ped.ReferencedBy) != 0 {
		t.Errorf("nadie apunta a pedidos: %+v", ped.ReferencedBy)
	}
}

// pg_total_relation_size NO recorre el árbol de particiones: sobre el padre
// devuelve el tamaño del padre, que está vacío. Sin sumar el árbol, una tabla
// particionada de cualquier tamaño mide cero.
func TestDetailMideLaTablaParticionadaEntera(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.medidas (
			id bigint NOT NULL,
			dia date NOT NULL,
			relleno text NOT NULL
		) PARTITION BY RANGE (dia);
		CREATE TABLE %[1]s.medidas_2026 PARTITION OF %[1]s.medidas
			FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
		INSERT INTO %[1]s.medidas
			SELECT g, date '2026-01-01' + (g %% 300), repeat('x', 200)
			FROM generate_series(1, 5000) g`, esq))

	d, err := Detail(context.Background(), pool, esq, "medidas")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if d.TotalBytes <= 0 {
		t.Fatalf("TotalBytes = %d: la partición tiene 5000 filas y el padre mide cero solo", d.TotalBytes)
	}
	if d.TableBytes <= 0 {
		t.Errorf("TableBytes = %d", d.TableBytes)
	}
	if d.TotalBytes < d.TableBytes {
		t.Errorf("TotalBytes (%d) < TableBytes (%d)", d.TotalBytes, d.TableBytes)
	}
	t.Logf("tabla particionada: total %d bytes, heap %d bytes", d.TotalBytes, d.TableBytes)
}

func TestDetailMideIndicesYHeapPorSeparado(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.datos (id bigint PRIMARY KEY, v text NOT NULL);
		INSERT INTO %[1]s.datos SELECT g, repeat('y', 100) FROM generate_series(1, 5000) g`, esq))

	d, err := Detail(context.Background(), pool, esq, "datos")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if d.TableBytes <= 0 || d.TotalBytes <= d.TableBytes {
		t.Fatalf("TotalBytes = %d, TableBytes = %d: la tabla tiene una clave primaria que ocupa lugar",
			d.TotalBytes, d.TableBytes)
	}
	if d.IndexBytes() <= 0 {
		t.Errorf("IndexBytes() = %d", d.IndexBytes())
	}
}

func TestDetailDaErrorClaroSiLaTablaNoExiste(t *testing.T) {
	pool, esq := conectar(t)

	_, err := Detail(context.Background(), pool, esq, "no_existe")
	if err == nil {
		t.Fatal("Detail() sobre una tabla inexistente no falló")
	}
	if !errors.Is(err, ErrTablaNoExiste) {
		t.Errorf("err = %v, se esperaba que envolviera ErrTablaNoExiste", err)
	}
}

// Las dos features de PostgreSQL 18 que Atlas no sabe leer. Se marcan en la
// introspección propia justamente porque Atlas no las ve: es lo que le va a
// permitir a la Iteración 5 negarse a generar DDL en vez de romper en silencio.
// Ver kaname-plan.md § 6.
func TestDetailMarcaLoQueAtlasNoVe(t *testing.T) {
	pool, esq := conectar(t)
	if v := versionMayor(t, pool); v < 18 {
		t.Skipf("las columnas VIRTUAL y el NOT NULL NOT VALID son de PostgreSQL 18; el servidor es %d", v)
	}

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.medidas (
			id bigint PRIMARY KEY,
			ancho numeric(10,2) NOT NULL,
			alto numeric(10,2) NOT NULL,
			area numeric(10,2) GENERATED ALWAYS AS (ancho * alto) VIRTUAL,
			volumen numeric(10,2) GENERATED ALWAYS AS (ancho * alto) STORED,
			email text
		);
		ALTER TABLE %[1]s.medidas ADD CONSTRAINT medidas_email_nn NOT NULL email NOT VALID`, esq))

	d, err := Detail(context.Background(), pool, esq, "medidas")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}

	porNombre := map[string]schema.DetailColumn{}
	for _, c := range d.Columns {
		porNombre[c.Name] = c
	}

	if c := porNombre["area"]; c.Generated != "virtual" {
		t.Errorf("area: Generated = %q, se esperaba \"virtual\" (Atlas la lee como stored)", c.Generated)
	}
	if c := porNombre["volumen"]; c.Generated != "stored" {
		t.Errorf("volumen: Generated = %q, se esperaba \"stored\"", c.Generated)
	}
	if c := porNombre["ancho"]; c.Generated != "" {
		t.Errorf("ancho: Generated = %q en una columna común", c.Generated)
	}

	email := porNombre["email"]
	if email.Nullable {
		t.Error("email: Nullable = true, pero la restricción NOT NULL existe")
	}
	if !email.NotNullNotValid {
		t.Error("email: NotNullNotValid = false, pero se agregó con NOT VALID: " +
			"las filas que ya estaban nunca se comprobaron")
	}
	if c := porNombre["ancho"]; c.NotNullNotValid {
		t.Error("ancho: NotNullNotValid = true en un NOT NULL declarado en el CREATE TABLE")
	}
}

// Dos tablas distintas pueden tener una restricción con el MISMO nombre, y las
// dos pueden apuntar a la misma. Sin el dueño, las dos claves entrantes son
// indistinguibles: la pantalla muestra dos filas idénticas y no hay forma de
// saber cuál de las dos borra en cascada.
func TestClavesEntrantesDicenQuienLasDeclara(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.destino (id bigint PRIMARY KEY);
		CREATE TABLE %[1]s.uno (
			id bigint PRIMARY KEY,
			destino_id bigint NOT NULL,
			CONSTRAINT mismo_nombre FOREIGN KEY (destino_id)
				REFERENCES %[1]s.destino (id) ON DELETE CASCADE
		);
		CREATE TABLE %[1]s.dos (
			id bigint PRIMARY KEY,
			destino_id bigint NOT NULL,
			CONSTRAINT mismo_nombre FOREIGN KEY (destino_id)
				REFERENCES %[1]s.destino (id) ON DELETE RESTRICT
		)`, esq))

	d, err := Detail(context.Background(), pool, esq, "destino")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if len(d.ReferencedBy) != 2 {
		t.Fatalf("hay %d claves entrantes, se esperaban 2: %+v", len(d.ReferencedBy), d.ReferencedBy)
	}

	porDueno := map[string]schema.ForeignKey{}
	for _, fk := range d.ReferencedBy {
		if fk.Schema != esq {
			t.Errorf("Schema = %q, se esperaba %q", fk.Schema, esq)
		}
		porDueno[fk.Table] = fk
	}
	if len(porDueno) != 2 {
		t.Fatalf("las dos claves entrantes dicen venir de la misma tabla: %+v", d.ReferencedBy)
	}
	if got := porDueno["uno"].OnDelete; got != schema.Cascade {
		t.Errorf("uno.mismo_nombre: OnDelete = %q, se esperaba cascade", got)
	}
	if got := porDueno["dos"].OnDelete; got != schema.Restrict {
		t.Errorf("dos.mismo_nombre: OnDelete = %q, se esperaba restrict", got)
	}

	// Y una clave saliente también sabe de quién es, aunque ahí sea obvio.
	u, err := Detail(context.Background(), pool, esq, "uno")
	if err != nil {
		t.Fatalf("Detail(uno) falló: %v", err)
	}
	if len(u.ForeignKeys) != 1 || u.ForeignKeys[0].Table != "uno" {
		t.Errorf("la clave saliente de uno dice ser de %+v", u.ForeignKeys)
	}
}

// Cada partición hereda la clave del padre con el mismo nombre. Sin filtrarlas,
// una tabla referenciada por otra particionada en cincuenta meses aparece con
// cincuenta y una claves entrantes idénticas.
func TestLasParticionesNoDuplicanLasClavesEntrantes(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.clientes (id bigint PRIMARY KEY);
		CREATE TABLE %[1]s.pedidos (
			id bigint,
			dia date NOT NULL,
			cliente_id bigint NOT NULL REFERENCES %[1]s.clientes (id),
			PRIMARY KEY (id, dia)
		) PARTITION BY RANGE (dia);
		CREATE TABLE %[1]s.pedidos_1 PARTITION OF %[1]s.pedidos
			FOR VALUES FROM ('2026-01-01') TO ('2026-07-01');
		CREATE TABLE %[1]s.pedidos_2 PARTITION OF %[1]s.pedidos
			FOR VALUES FROM ('2026-07-01') TO ('2027-01-01')`, esq))

	d, err := Detail(context.Background(), pool, esq, "clientes")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if len(d.ReferencedBy) != 1 {
		t.Fatalf("hay %d claves entrantes, se esperaba 1 (las 2 particiones heredan la del padre): %+v",
			len(d.ReferencedBy), d.ReferencedBy)
	}
	if d.ReferencedBy[0].Table != "pedidos" {
		t.Errorf("la clave entrante viene de %q y no de la tabla particionada padre",
			d.ReferencedBy[0].Table)
	}
}

// indkey trae también las columnas de INCLUDE, así que preguntar por el vector
// entero marca como clave primaria a una columna que solo viaja adentro del
// índice.
func TestLasColumnasDeIncludeNoSonClavePrimaria(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.t (
			id bigint,
			extra text NOT NULL,
			otra int,
			PRIMARY KEY (id) INCLUDE (extra)
		)`, esq))

	d, err := Detail(context.Background(), pool, esq, "t")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	porNombre := map[string]schema.DetailColumn{}
	for _, c := range d.Columns {
		porNombre[c.Name] = c
	}
	if !porNombre["id"].PrimaryKey {
		t.Error("id: PrimaryKey = false y es la clave")
	}
	if porNombre["extra"].PrimaryKey {
		t.Error("extra: PrimaryKey = true y es una columna de INCLUDE, no una clave")
	}
	if porNombre["otra"].PrimaryKey {
		t.Error("otra: PrimaryKey = true y no está en el índice")
	}

	// El snapshot usa otra consulta para lo mismo; tiene que decir igual.
	snap, err := Introspect(context.Background(), pool)
	if err != nil {
		t.Fatalf("Introspect() falló: %v", err)
	}
	for _, c := range buscarTabla(t, snap, esq, "t").Columns {
		if quiero := c.Name == "id"; c.PrimaryKey != quiero {
			t.Errorf("snapshot, %s: PrimaryKey = %v, se esperaba %v", c.Name, c.PrimaryKey, quiero)
		}
	}
}

// El índice de una tabla particionada no guarda nada él mismo: pg_relation_size
// devuelve cero y hay que sumar el árbol, igual que con la tabla.
func TestLosIndicesDeUnaParticionadaMidenElArbol(t *testing.T) {
	pool, esq := conectar(t)

	ejecutar(t, pool, fmt.Sprintf(`
		CREATE TABLE %[1]s.p (id bigint NOT NULL, dia date NOT NULL, v text NOT NULL)
			PARTITION BY RANGE (dia);
		CREATE TABLE %[1]s.p1 PARTITION OF %[1]s.p
			FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
		INSERT INTO %[1]s.p SELECT g, date '2026-02-01', repeat('z', 80)
			FROM generate_series(1, 20000) g;
		CREATE INDEX p_id_idx ON %[1]s.p (id)`, esq))

	d, err := Detail(context.Background(), pool, esq, "p")
	if err != nil {
		t.Fatalf("Detail() falló: %v", err)
	}
	if len(d.Indexes) != 1 {
		t.Fatalf("hay %d índices, se esperaba 1", len(d.Indexes))
	}
	if d.Indexes[0].SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d: el índice de la partición tiene 20.000 filas y el padre no guarda nada",
			d.Indexes[0].SizeBytes)
	}
	if !d.Indexes[0].Valid {
		t.Error("Valid = false en un índice recién creado")
	}
	t.Logf("índice particionado: %d bytes", d.Indexes[0].SizeBytes)
}
