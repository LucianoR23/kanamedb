package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/drift"
	"github.com/LucianoR23/kanamedb/internal/layout"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"

	_ "modernc.org/sqlite"
)

// dosSQLite arma una sesión con dos conexiones a dos archivos SQLite con
// esquemas distintos, para comparar de verdad sin necesitar un servidor.
//
// SQLite no es la opción cómoda sino la exigente: es el único motor cuyo
// renderizador necesita la conexión ABIERTA —una clave foránea nueva
// reconstruye la tabla, y para eso lee la definición que hay—, así que si el
// destino se cerrara antes de escribir las sentencias, acá se nota.
//
// Origen (lo que se quiere) contra destino (lo que hay):
//
//	origen: clientes(id, nombre NOT NULL, email)   productos(id, nombre)   pedidos(id, producto_id → productos)   vista v_clientes
//	destino: clientes(id, nombre)                                           pedidos(id, producto_id)   viejo(id)
//
// `clientes` recibe dos cambios, y uno de ellos —el NOT NULL— reconstruye la
// tabla: es el caso que el archivo no puede llevar entero. Ver el test de
// reconstrucciones en conflicto.
func dosSQLite(t *testing.T) (*Session, string, string) {
	t.Helper()
	dir := t.TempDir()

	crear := func(nombre string, ddl ...string) string {
		ruta := filepath.ToSlash(filepath.Join(dir, nombre+".db"))
		db, err := sql.Open("sqlite", "file:"+ruta)
		if err != nil {
			t.Fatalf("abrir %s: %v", nombre, err)
		}
		defer db.Close()
		for _, s := range ddl {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("%s: %s: %v", nombre, s, err)
			}
		}
		return ruta
	}
	origen := crear("origen",
		`CREATE TABLE clientes (id INTEGER PRIMARY KEY, nombre TEXT NOT NULL, email TEXT)`,
		`CREATE TABLE productos (id INTEGER PRIMARY KEY, nombre TEXT)`,
		`CREATE TABLE pedidos (id INTEGER PRIMARY KEY, producto_id INTEGER,
			FOREIGN KEY (producto_id) REFERENCES productos(id))`,
		`CREATE VIEW v_clientes AS SELECT id, nombre FROM clientes`,
	)
	destino := crear("destino",
		`CREATE TABLE clientes (id INTEGER PRIMARY KEY, nombre TEXT)`,
		`CREATE TABLE pedidos (id INTEGER PRIMARY KEY, producto_id INTEGER)`,
		`CREATE TABLE viejo (id INTEGER PRIMARY KEY)`,
	)

	st := store.New(filepath.Join(dir, "connections.toml"))
	for id, ruta := range map[string]string{"origen01": origen, "destino01": destino} {
		c := connection.Connection{
			ID: id, Name: id, Engine: connection.SQLite, Database: ruta,
			Environment: connection.Local,
		}
		if err := st.Add(c.Normalize()); err != nil {
			t.Fatalf("Add(%s): %v", id, err)
		}
	}
	sesion := NewSession(st, newFakeKeyring(),
		tunnel.NewKnownHosts(filepath.Join(dir, "known_hosts")),
		layout.New(filepath.Join(dir, "layouts")))
	t.Cleanup(sesion.Disconnect)
	return sesion, "origen01", "destino01"
}

func compararDosSQLite(t *testing.T) (*Session, CompareResult) {
	t.Helper()
	sesion, origen, destino := dosSQLite(t)
	res := sesion.Compare(context.Background(), CompareRequest{SourceID: origen, TargetID: destino})
	if !res.OK {
		t.Fatalf("Compare(): %+v", res.Failure)
	}
	return sesion, res
}

func diferencia(t *testing.T, res CompareResult, id string) drift.Diferencia {
	t.Helper()
	for _, d := range res.Result.Diferencias {
		if d.ID == id {
			return d
		}
	}
	var ids []string
	for _, d := range res.Result.Diferencias {
		ids = append(ids, d.ID)
	}
	t.Fatalf("no está la diferencia %q; hay: %s", id, strings.Join(ids, ", "))
	return drift.Diferencia{}
}

// Toda diferencia con operación viaja con su sentencia, escrita por el motor
// del destino; toda diferencia sin operación viaja con el motivo. No hay un
// tercer estado.
func TestCadaDiferenciaConOperacionTraeSuSentencia(t *testing.T) {
	_, res := compararDosSQLite(t)

	if res.ID == "" {
		t.Error("la comparación no trae ID, y sin él no se puede pedir la migración")
	}
	conOperacion := 0
	for _, d := range res.Result.Diferencias {
		st, hay := res.Statements[d.ID]
		switch {
		case d.Cambio != nil && !hay:
			t.Errorf("%s tiene operación y no tiene sentencia", d.ID)
		case d.Cambio == nil && hay:
			t.Errorf("%s no tiene operación y sí tiene sentencia: %q", d.ID, st.SQL)
		case d.Cambio == nil && d.SinSentencia == "":
			t.Errorf("%s no tiene operación ni dice por qué", d.ID)
		case hay && st.SQL == "":
			t.Errorf("%s trae una sentencia vacía", d.ID)
		case hay && st.ChangeID != d.ID:
			t.Errorf("la sentencia de %s apunta al cambio %q", d.ID, st.ChangeID)
		}
		if d.Cambio != nil {
			conOperacion++
		}
	}
	if conOperacion == 0 {
		t.Fatal("el fixture no produjo ninguna operación: el test no está probando nada")
	}

	// Las tres que se esperan, y cómo se escribieron.
	tabla := diferencia(t, res, "table:main.productos")
	if tabla.Lado != drift.SoloEnOrigen || tabla.Cambio == nil {
		t.Errorf("productos: %+v", tabla)
	}
	if sql := res.Statements[tabla.ID].SQL; !strings.HasPrefix(sql, "CREATE TABLE") {
		t.Errorf("la tabla nueva no salió como CREATE TABLE: %q", sql)
	}

	columna := diferencia(t, res, "column:main.clientes.email")
	if sql := res.Statements[columna.ID].SQL; !strings.Contains(sql, "ADD COLUMN") {
		t.Errorf("la columna nueva no salió como ADD COLUMN: %q", sql)
	}

	// La clave foránea es la que necesita el destino ABIERTO: en SQLite se
	// agrega reconstruyendo la tabla, y para eso el motor lee lo que hay.
	fk := diferencia(t, res, "fk:main.pedidos.producto_id→productos(id)")
	stFK, hay := res.Statements[fk.ID]
	if !hay {
		t.Fatalf("la clave foránea quedó sin sentencia: %q", fk.SinSentencia)
	}
	if !stFK.RebuildsTable || !strings.Contains(stFK.SQL, "FOREIGN KEY") {
		t.Errorf("la clave foránea no salió como reconstrucción con FOREIGN KEY: %+v", stFK)
	}

	// Y lo que solo está en el destino no tiene sentencia, con motivo.
	viejo := diferencia(t, res, "table:main.viejo")
	if viejo.Cambio != nil || viejo.SinSentencia == "" {
		t.Errorf("la tabla que sobra en el destino tendría que venir sin sentencia y con motivo: %+v", viejo)
	}

	// La vista que falta en el destino se reporta. Introspect no trae los
	// objetos que no son tablas —los pega la sesión después— y la primera
	// versión de Compare no lo hacía: la comparación decía que miraba las
	// vistas por nombre y no miraba ninguna. Lo encontró la prueba a mano.
	vista := diferencia(t, res, "object:main.view:v_clientes")
	if vista.Lado != drift.SoloEnOrigen || vista.Clase != drift.ClaseObjeto || vista.SinSentencia == "" {
		t.Errorf("la vista que falta en el destino no se reportó como corresponde: %+v", vista)
	}
}

// Si el motor del destino no sabe escribir una operación, la diferencia se
// devuelve sin operación y con el motivo, y no con una operación que después
// nadie puede usar.
func TestUnaOperacionQueElMotorNoSabeEscribirSeReportaSinSentencia(t *testing.T) {
	sesion, origen, destino := dosSQLite(t)
	abierta, failure := sesion.abrirConexion(context.Background(), destino, "")
	if failure != nil {
		t.Fatalf("abrir el destino: %+v", failure)
	}
	defer abierta.cerrar()
	_ = origen

	res := drift.Resultado{Diferencias: []drift.Diferencia{{
		ID: "column:main.clientes.email", Lado: drift.SoloEnOrigen, Clase: drift.ClaseColumna,
		Cambio: &change.Change{
			Type: change.AddColumn, Schema: "main", Table: "clientes",
			// Un tipo con punto y coma adentro: el renderizador lo rechaza
			// porque es la única parte del DDL que se concatena.
			Column: &change.Column{Name: "email", DataType: "text; drop table clientes", Nullable: true},
		},
	}, {
		ID: "table:main.productos", Lado: drift.SoloEnOrigen, Clase: drift.ClaseTabla,
		Cambio: &change.Change{
			Type: change.CreateTable, Schema: "main", Table: "productos",
			Columns: []change.Column{{Name: "id", DataType: "integer", Nullable: false}},
			Names:   []string{"id"},
		},
	}}}

	sentencias := renderizarDiferencias(context.Background(), abierta.db, &res)

	mala := res.Diferencias[0]
	if mala.Cambio != nil {
		t.Error("la operación que no se pudo escribir sigue ofrecida")
	}
	if mala.SinSentencia == "" {
		t.Error("no se dijo por qué no hay sentencia")
	}
	if _, hay := sentencias[mala.ID]; hay {
		t.Error("hay sentencia para una operación que el motor rechazó")
	}
	buena := res.Diferencias[1]
	if buena.Cambio == nil || sentencias[buena.ID].SQL == "" {
		t.Errorf("la operación válida se perdió con la otra: %+v / %+v", buena, sentencias[buena.ID])
	}
}

// La migración es lo que la pantalla mostró, en un orden que se puede correr.
func TestLaMigracionSaleDeLaComparacionYEnOrdenEjecutable(t *testing.T) {
	sesion, res := compararDosSQLite(t)

	var todas []string
	for _, d := range res.Result.Diferencias {
		todas = append(todas, d.ID)
	}
	// En orden inverso al de la comparación: el archivo no puede depender del
	// orden en que la interfaz mande los IDs.
	for i, j := 0, len(todas)-1; i < j; i, j = i+1, j-1 {
		todas[i], todas[j] = todas[j], todas[i]
	}

	m, err := sesion.Migration(MigrationRequest{CompareID: res.ID, Include: todas})
	if err != nil {
		t.Fatalf("Migration(): %v", err)
	}
	if m.Statements != 3 || m.Commented != 3 {
		t.Errorf("Statements=%d Commented=%d; se esperaban 3 y 3", m.Statements, m.Commented)
	}

	// Toda sentencia de la comparación está en el archivo, tal cual.
	for id, st := range res.Statements {
		if !strings.Contains(m.SQL, strings.TrimRight(st.SQL, ";\n")) {
			t.Errorf("la sentencia de %s no está en el archivo:\n%s", id, st.SQL)
		}
	}
	// Y la diferencia sin sentencia está, comentada, con el motivo. Los
	// comentarios se parten en líneas, así que se compara sin los saltos.
	viejo := diferencia(t, res, "table:main.viejo")
	if !strings.Contains(m.SQL, "-- [-] tabla main.viejo") ||
		!strings.Contains(textoPlano(m.SQL), textoPlano(viejo.SinSentencia)) {
		t.Errorf("la tabla que sobra en el destino no quedó comentada con su motivo:\n%s", m.SQL)
	}

	// El orden: CREATE TABLE productos ANTES de la clave foránea que apunta a
	// ella, aunque «pedidos» venga antes que «productos» en la comparación y
	// aunque la interfaz haya mandado los IDs al revés.
	crear := strings.Index(m.SQL, "CREATE TABLE \"productos\"")
	fk := strings.Index(m.SQL, "FOREIGN KEY")
	if crear < 0 || fk < 0 {
		t.Fatalf("faltan sentencias en el archivo:\n%s", m.SQL)
	}
	if fk < crear {
		t.Errorf("la clave foránea está antes que la tabla a la que apunta:\n%s", m.SQL)
	}

	// Cada bloque con sentencia termina en punto y coma: el archivo se corre
	// entero, también la reconstrucción de SQLite, que trae varias.
	bloques := strings.Split(m.SQL, "\n\n")
	for _, bloque := range bloques[1:] {
		bloque = strings.TrimSpace(bloque)
		if soloComentarios(bloque) {
			continue // una diferencia sin sentencia: no hay nada que terminar
		}
		if !strings.HasSuffix(bloque, ";") {
			t.Errorf("un bloque no termina en punto y coma:\n%s", bloque)
		}
	}

	// Ninguna sentencia borra nada. Es la regla del paquete, y el archivo es
	// donde de verdad importa: es lo que alguien va a correr.
	for _, st := range res.Statements {
		if st.Destructive {
			t.Errorf("la migración lleva una sentencia destructiva: %s", st.SQL)
		}
	}
	for _, linea := range strings.Split(m.SQL, "\n") {
		l := strings.ToUpper(strings.TrimSpace(linea))
		if strings.HasPrefix(l, "--") {
			continue
		}
		// La reconstrucción de SQLite SÍ tiene un DROP TABLE adentro, sobre la
		// copia que acaba de llenar. Es la única forma que hay, y viene marcada
		// como RebuildsTable y no como destructiva.
		if strings.HasPrefix(l, "DROP ") && !strings.Contains(m.SQL, "se reconstruye la tabla") {
			t.Errorf("hay un DROP suelto en la migración: %s", linea)
		}
	}
}

// textoPlano saca los marcadores de comentario y junta los espacios, para
// buscar un texto que el archivo parte en varias líneas.
func textoPlano(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "--", " ")), " ")
}

// soloComentarios dice si todas las líneas de un bloque son comentarios.
func soloComentarios(bloque string) bool {
	for _, l := range strings.Split(bloque, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "--") {
			return false
		}
	}
	return true
}

func TestLaMigracionRechazaLoQueNoEsDeEstaComparacion(t *testing.T) {
	sesion, res := compararDosSQLite(t)
	alguna := res.Result.Diferencias[0].ID

	casos := []struct {
		nombre string
		req    MigrationRequest
	}{
		{"sin comparación", MigrationRequest{Include: []string{alguna}}},
		{"otra comparación", MigrationRequest{CompareID: "otra", Include: []string{alguna}}},
		{"sin diferencias", MigrationRequest{CompareID: res.ID}},
		{"diferencia inventada", MigrationRequest{CompareID: res.ID, Include: []string{alguna, "table:main.inventada"}}},
	}
	for _, c := range casos {
		if _, err := sesion.Migration(c.req); err == nil {
			t.Errorf("%s: se generó una migración", c.nombre)
		}
	}

	// Y una comparación NUEVA invalida el ID de la anterior: el archivo tiene
	// que salir de lo que se está mirando.
	otra := sesion.Compare(context.Background(), CompareRequest{SourceID: "origen01", TargetID: "destino01"})
	if !otra.OK {
		t.Fatalf("segunda comparación: %+v", otra.Failure)
	}
	if otra.ID == res.ID {
		t.Fatal("dos comparaciones tienen el mismo ID")
	}
	if _, err := sesion.Migration(MigrationRequest{CompareID: res.ID, Include: []string{alguna}}); err == nil {
		t.Error("se generó la migración de una comparación que ya fue reemplazada")
	}
}

// El archivo se escribe entero o no se escribe: un fallo a mitad de camino no
// deja un archivo cortado con el nombre que la persona eligió.
func TestGuardarLaMigracionEsAtomico(t *testing.T) {
	sesion, res := compararDosSQLite(t)
	var todas []string
	for _, d := range res.Result.Diferencias {
		todas = append(todas, d.ID)
	}
	dir := t.TempDir()
	ruta := filepath.Join(dir, "migracion.sql")

	info, err := sesion.SaveMigration(MigrationRequest{CompareID: res.ID, Include: todas}, ruta)
	if err != nil {
		t.Fatalf("SaveMigration(): %v", err)
	}
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("leer el archivo: %v", err)
	}
	if int64(len(contenido)) != info.Bytes || info.Bytes == 0 {
		t.Errorf("Bytes=%d y el archivo tiene %d", info.Bytes, len(contenido))
	}
	if info.Statements != 3 || info.Commented != 3 {
		t.Errorf("Statements=%d Commented=%d", info.Statements, info.Commented)
	}
	if !strings.HasPrefix(string(contenido), "-- Migración generada por Kaname") {
		t.Errorf("el archivo no arranca con la cabecera:\n%.80s", contenido)
	}

	// No queda ningún temporal en el directorio.
	entradas, _ := os.ReadDir(dir)
	if len(entradas) != 1 {
		var nombres []string
		for _, e := range entradas {
			nombres = append(nombres, e.Name())
		}
		t.Errorf("quedaron archivos de más en el directorio: %v", nombres)
	}

	// Un directorio que no existe: no se escribe nada, en ningún lado.
	if _, err := sesion.SaveMigration(MigrationRequest{CompareID: res.ID, Include: todas},
		filepath.Join(dir, "no-existe", "m.sql")); err == nil {
		t.Error("se guardó en un directorio que no existe")
	}
	if _, err := sesion.SaveMigration(MigrationRequest{CompareID: res.ID, Include: todas}, ""); err == nil {
		t.Error("se guardó sin ruta")
	}
}

// El texto del archivo no depende de la hora salvo en la cabecera, y una
// cabecera fija permite comparar dos corridas. Además: NADA de credenciales
// adentro. Los lados se describen como usuario@host/base.
func TestLaCabeceraDeLaMigracionDescribeLosLadosSinSecretos(t *testing.T) {
	cmp := &CompareResult{
		OK: true, ID: "c1",
		Result: &drift.Resultado{Diferencias: []drift.Diferencia{{
			ID: "table:public.t", Lado: drift.SoloEnOrigen, Clase: drift.ClaseTabla,
			Schema: "public", Objeto: "t", Resumen: "la tabla no existe en el destino",
			Cambio: &change.Change{Type: change.CreateTable},
		}}},
		Statements: map[string]change.Statement{
			"table:public.t": {SQL: "CREATE TABLE t (id int)"},
		},
		Source: CompareSide{Name: "dev", Engine: "postgres", Environment: "dev", Describe: "app@dev:5432/shop"},
		Target: CompareSide{Name: "prod", Engine: "postgres", Environment: "production", Production: true, Describe: "app@prod:5432/shop"},
	}
	m, err := armarMigracion(cmp, []string{"table:public.t"}, time.Date(2026, 9, 11, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("armarMigracion(): %v", err)
	}
	quiero := []string{
		"-- Migración generada por Kaname el 2026-09-11 10:30",
		"-- Origen:  dev (postgres, dev) · app@dev:5432/shop",
		"-- Destino: prod (postgres, PRODUCCIÓN) · app@prod:5432/shop",
		"-- 1 sentencia\n",
		"-- [+] tabla public.t\n",
		"CREATE TABLE t (id int);\n",
		// Postgres respeta la transacción: el archivo va entre BEGIN y COMMIT.
		"BEGIN;\n",
		"COMMIT;\n",
	}
	for _, q := range quiero {
		if !strings.Contains(m.SQL, q) {
			t.Errorf("falta %q en:\n%s", q, m.SQL)
		}
	}
	if strings.Contains(m.SQL, "PRAGMA") {
		t.Error("un archivo para Postgres lleva pragmas de SQLite")
	}
	if strings.Index(m.SQL, "BEGIN;") > strings.Index(m.SQL, "CREATE TABLE") ||
		strings.Index(m.SQL, "COMMIT;") < strings.Index(m.SQL, "CREATE TABLE") {
		t.Errorf("la sentencia no está entre el BEGIN y el COMMIT:\n%s", m.SQL)
	}
}

// Una reconstrucción de tabla se escribe desde la definición que hay AHORA. Si
// en el archivo la precede otra sentencia sobre la misma tabla, la
// reconstrucción no la conoce y la deshace sin avisar: un ADD COLUMN seguido
// del rebuild de un SET NOT NULL crea la copia sin la columna nueva. Lo
// encontró el review; el fixture tiene el caso a propósito.
func TestUnaReconstruccionDespuesDeOtraSentenciaSobreLaTablaNoVaAlArchivo(t *testing.T) {
	sesion, res := compararDosSQLite(t)

	email := diferencia(t, res, "column:main.clientes.email")
	if email.Cambio == nil || res.Statements[email.ID].SQL == "" {
		t.Fatalf("el ADD COLUMN, que va primero, tendría que seguir: %+v", email)
	}
	nombre := diferencia(t, res, "columnNull:main.clientes.nombre")
	if nombre.Lado != drift.Distinto {
		t.Fatalf("nombre tendría que ser «distinto» (NOT NULL de un lado): %+v", nombre)
	}
	if nombre.Cambio != nil {
		t.Errorf("la reconstrucción que va después del ADD COLUMN sigue ofrecida: %+v", nombre.Cambio)
	}
	if _, hay := res.Statements[nombre.ID]; hay {
		t.Error("hay sentencia para la reconstrucción en conflicto")
	}
	if !strings.Contains(nombre.SinSentencia, "reconstruye") || !strings.Contains(nombre.SinSentencia, "volvé a comparar") {
		t.Errorf("el motivo no explica el conflicto ni el camino: %q", nombre.SinSentencia)
	}

	// La clave foránea de pedidos también reconstruye, pero es la ÚNICA
	// sentencia sobre pedidos: se queda.
	fk := diferencia(t, res, "fk:main.pedidos.producto_id→productos(id)")
	if fk.Cambio == nil {
		t.Errorf("una reconstrucción sola sobre su tabla se suprimió sin motivo: %+v", fk)
	}

	// Y el archivo lleva la reconstrucción con su envoltura: claves foráneas
	// apagadas ANTES del BEGIN, verificación antes del COMMIT, y todo restaurado
	// al final. Sin eso, el DROP TABLE del rebuild dispara los ON DELETE
	// CASCADE de las tablas hijas en cualquier cliente con las claves prendidas.
	var todas []string
	for _, d := range res.Result.Diferencias {
		todas = append(todas, d.ID)
	}
	m, err := sesion.Migration(MigrationRequest{CompareID: res.ID, Include: todas})
	if err != nil {
		t.Fatalf("Migration(): %v", err)
	}
	orden := []string{
		"PRAGMA foreign_keys = OFF;",
		"PRAGMA legacy_alter_table = ON;",
		"BEGIN;",
		"CREATE TABLE",
		"FOREIGN KEY",
		"PRAGMA foreign_key_check;",
		"COMMIT;",
		"PRAGMA legacy_alter_table = OFF;",
		"PRAGMA foreign_keys = ON;",
	}
	pos := -1
	for _, q := range orden {
		i := strings.Index(m.SQL, q)
		if i < 0 {
			t.Fatalf("falta %q en el archivo:\n%s", q, m.SQL)
		}
		if i < pos {
			t.Errorf("%q está antes de lo que lo precede en el archivo:\n%s", q, m.SQL)
		}
		pos = i
	}
}

// En MySQL y MariaDB cada DDL confirma solo: el archivo no promete una
// transacción que el motor no respeta, y lo dice.
func TestLaMigracionParaMySQLNoPrometeUnaTransaccion(t *testing.T) {
	cmp := &CompareResult{
		OK: true, ID: "c1",
		Result: &drift.Resultado{Diferencias: []drift.Diferencia{{
			ID: "table:db.t", Lado: drift.SoloEnOrigen, Clase: drift.ClaseTabla,
			Schema: "db", Objeto: "t", Resumen: "la tabla no existe en el destino",
			Cambio: &change.Change{Type: change.CreateTable},
		}}},
		Statements: map[string]change.Statement{"table:db.t": {SQL: "CREATE TABLE t (id int)"}},
		Source:     CompareSide{Name: "a", Engine: "mysql", Environment: "dev", Describe: "u@a:3306/db"},
		Target:     CompareSide{Name: "b", Engine: "mariadb", Environment: "dev", Describe: "u@b:3306/db"},
	}
	m, err := armarMigracion(cmp, []string{"table:db.t"}, time.Now())
	if err != nil {
		t.Fatalf("armarMigracion(): %v", err)
	}
	if strings.Contains(m.SQL, "BEGIN;") || strings.Contains(m.SQL, "COMMIT;") || strings.Contains(m.SQL, "PRAGMA") {
		t.Errorf("el archivo para MariaDB promete una transacción:\n%s", m.SQL)
	}
	if !strings.Contains(m.SQL, "cada sentencia de DDL confirma sola") {
		t.Errorf("el archivo no avisa que no hay transacción:\n%s", m.SQL)
	}
}

// Una clave foránea hacia una tabla que recién recibe su clave primaria en la
// misma migración falla si la foránea va antes: Postgres y MySQL piden una
// restricción única en la tabla referida. La primaria va con las columnas.
func TestLaClavePrimariaVaAntesQueLasForaneas(t *testing.T) {
	cmp := &CompareResult{
		OK: true, ID: "c1",
		Result: &drift.Resultado{Diferencias: []drift.Diferencia{{
			// La foránea aparece ANTES en la comparación (por nombre de tabla).
			ID: "fk:public.libros.libros_autor_fk", Lado: drift.SoloEnOrigen, Clase: drift.ClaseForanea,
			Schema: "public", Objeto: "libros → libros_autor_fk", Resumen: "la clave foránea no existe en el destino",
			Cambio: &change.Change{Type: change.AddForeignKey, Schema: "public", Table: "libros"},
		}, {
			ID: "pk:public.autores", Lado: drift.SoloEnOrigen, Clase: drift.ClaseTabla,
			Schema: "public", Objeto: "autores", Resumen: "la clave primaria no existe en el destino",
			Cambio: &change.Change{Type: change.AddPrimaryKey, Schema: "public", Table: "autores"},
		}}},
		Statements: map[string]change.Statement{
			"fk:public.libros.libros_autor_fk": {SQL: "ALTER TABLE libros ADD CONSTRAINT libros_autor_fk FOREIGN KEY (autor_id) REFERENCES autores (id)"},
			"pk:public.autores":                {SQL: "ALTER TABLE autores ADD PRIMARY KEY (id)"},
		},
		Source: CompareSide{Engine: "postgres"},
		Target: CompareSide{Engine: "postgres"},
	}
	m, err := armarMigracion(cmp, []string{"fk:public.libros.libros_autor_fk", "pk:public.autores"}, time.Now())
	if err != nil {
		t.Fatalf("armarMigracion(): %v", err)
	}
	if strings.Index(m.SQL, "ADD PRIMARY KEY") > strings.Index(m.SQL, "FOREIGN KEY") {
		t.Errorf("la clave foránea está antes que la primaria de la tabla a la que apunta:\n%s", m.SQL)
	}
}
