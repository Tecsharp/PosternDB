-- Plantillas "compartidas": las que el propietario marca como visibles para
-- los usuarios registrados, que pueden crear su base a partir de ellas.
ALTER TABLE templates ADD COLUMN IF NOT EXISTS shared BOOLEAN NOT NULL DEFAULT false;

UPDATE templates SET shared = true WHERE name = 'Contactos (demo)';

INSERT INTO templates (name, description, sql, shared) VALUES (
    'Tareas (to-do)',
    'Lista de tareas con estado y fecha límite. Ideal para empezar una app sencilla.',
    $tpl$CREATE TABLE tareas (
    id          BIGSERIAL PRIMARY KEY,
    titulo      TEXT        NOT NULL,
    detalle     TEXT,
    completada  BOOLEAN     NOT NULL DEFAULT false,
    vence_el    DATE,
    creada_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO tareas (titulo, detalle, completada, vence_el) VALUES
    ('Crear mi primera base de datos', 'Ya lo hiciste, felicidades.', true, NULL),
    ('Conectar mi aplicación', 'Usa los datos de la pestaña Conexión.', false, CURRENT_DATE + 7),
    ('Respaldar mis datos', NULL, false, CURRENT_DATE + 30);$tpl$,
    true
) ON CONFLICT (name) DO NOTHING;

INSERT INTO templates (name, description, sql, shared) VALUES (
    'Blog',
    'Autores, entradas y comentarios con relaciones entre tablas.',
    $tpl$CREATE TABLE autores (
    id      BIGSERIAL PRIMARY KEY,
    nombre  TEXT NOT NULL,
    email   TEXT NOT NULL UNIQUE
);
CREATE TABLE entradas (
    id           BIGSERIAL PRIMARY KEY,
    autor_id     BIGINT      NOT NULL REFERENCES autores (id) ON DELETE CASCADE,
    titulo       TEXT        NOT NULL,
    contenido    TEXT        NOT NULL,
    publicada    BOOLEAN     NOT NULL DEFAULT false,
    publicada_en TIMESTAMPTZ,
    creada_en    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE comentarios (
    id         BIGSERIAL PRIMARY KEY,
    entrada_id BIGINT      NOT NULL REFERENCES entradas (id) ON DELETE CASCADE,
    nombre     TEXT        NOT NULL,
    texto      TEXT        NOT NULL,
    creado_en  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX comentarios_entrada ON comentarios (entrada_id);
INSERT INTO autores (nombre, email) VALUES ('Laura Gómez', 'laura@example.com');
INSERT INTO entradas (autor_id, titulo, contenido, publicada, publicada_en)
    VALUES (1, 'Hola mundo', 'Mi primera entrada.', true, now());
INSERT INTO comentarios (entrada_id, nombre, texto) VALUES (1, 'Pedro', '¡Bienvenida!');$tpl$,
    true
) ON CONFLICT (name) DO NOTHING;

INSERT INTO templates (name, description, sql, shared) VALUES (
    'Tienda en línea',
    'Productos, clientes y pedidos con sus líneas de detalle.',
    $tpl$CREATE TABLE productos (
    id      BIGSERIAL PRIMARY KEY,
    nombre  TEXT           NOT NULL,
    precio  NUMERIC(10, 2) NOT NULL CHECK (precio >= 0),
    stock   INTEGER        NOT NULL DEFAULT 0 CHECK (stock >= 0),
    activo  BOOLEAN        NOT NULL DEFAULT true
);
CREATE TABLE clientes (
    id        BIGSERIAL PRIMARY KEY,
    nombre    TEXT NOT NULL,
    email     TEXT NOT NULL UNIQUE,
    telefono  TEXT
);
CREATE TABLE pedidos (
    id         BIGSERIAL PRIMARY KEY,
    cliente_id BIGINT      NOT NULL REFERENCES clientes (id),
    estado     TEXT        NOT NULL DEFAULT 'pendiente',
    creado_en  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE pedido_lineas (
    pedido_id       BIGINT         NOT NULL REFERENCES pedidos (id) ON DELETE CASCADE,
    producto_id     BIGINT         NOT NULL REFERENCES productos (id),
    cantidad        INTEGER        NOT NULL CHECK (cantidad > 0),
    precio_unitario NUMERIC(10, 2) NOT NULL,
    PRIMARY KEY (pedido_id, producto_id)
);
INSERT INTO productos (nombre, precio, stock) VALUES
    ('Camiseta', 249.00, 40), ('Taza', 129.50, 25), ('Libreta', 79.90, 100);
INSERT INTO clientes (nombre, email) VALUES ('Marta Ruiz', 'marta@example.com');
INSERT INTO pedidos (cliente_id, estado) VALUES (1, 'pagado');
INSERT INTO pedido_lineas (pedido_id, producto_id, cantidad, precio_unitario) VALUES (1, 1, 2, 249.00), (1, 3, 1, 79.90);$tpl$,
    true
) ON CONFLICT (name) DO NOTHING;

INSERT INTO templates (name, description, sql, shared) VALUES (
    'Agenda de citas',
    'Servicios, clientes y citas para un negocio con reservas.',
    $tpl$CREATE TABLE servicios (
    id        BIGSERIAL PRIMARY KEY,
    nombre    TEXT           NOT NULL,
    minutos   INTEGER        NOT NULL CHECK (minutos > 0),
    precio    NUMERIC(10, 2) NOT NULL DEFAULT 0
);
CREATE TABLE clientes (
    id        BIGSERIAL PRIMARY KEY,
    nombre    TEXT NOT NULL,
    telefono  TEXT,
    email     TEXT
);
CREATE TABLE citas (
    id          BIGSERIAL PRIMARY KEY,
    cliente_id  BIGINT      NOT NULL REFERENCES clientes (id) ON DELETE CASCADE,
    servicio_id BIGINT      NOT NULL REFERENCES servicios (id),
    inicia_en   TIMESTAMPTZ NOT NULL,
    estado      TEXT        NOT NULL DEFAULT 'reservada',
    notas       TEXT
);
CREATE INDEX citas_inicia ON citas (inicia_en);
INSERT INTO servicios (nombre, minutos, precio) VALUES ('Consulta', 30, 400), ('Seguimiento', 15, 200);
INSERT INTO clientes (nombre, telefono) VALUES ('Sofía Reyes', '5598765432');
INSERT INTO citas (cliente_id, servicio_id, inicia_en) VALUES (1, 1, now() + interval '2 days');$tpl$,
    true
) ON CONFLICT (name) DO NOTHING;

INSERT INTO templates (name, description, sql, shared) VALUES (
    'Inventario',
    'Artículos, ubicaciones y movimientos de entrada y salida.',
    $tpl$CREATE TABLE ubicaciones (
    id      BIGSERIAL PRIMARY KEY,
    nombre  TEXT NOT NULL UNIQUE
);
CREATE TABLE articulos (
    id            BIGSERIAL PRIMARY KEY,
    sku           TEXT    NOT NULL UNIQUE,
    nombre        TEXT    NOT NULL,
    ubicacion_id  BIGINT  REFERENCES ubicaciones (id),
    minimo        INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE movimientos (
    id           BIGSERIAL PRIMARY KEY,
    articulo_id  BIGINT      NOT NULL REFERENCES articulos (id) ON DELETE CASCADE,
    cantidad     INTEGER     NOT NULL CHECK (cantidad <> 0),
    motivo       TEXT,
    hecho_en     TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO ubicaciones (nombre) VALUES ('Bodega A'), ('Mostrador');
INSERT INTO articulos (sku, nombre, ubicacion_id, minimo) VALUES ('TOR-001', 'Tornillos 1/4"', 1, 50), ('CAB-010', 'Cable USB-C', 2, 10);
INSERT INTO movimientos (articulo_id, cantidad, motivo) VALUES (1, 200, 'compra inicial'), (2, 30, 'compra inicial'), (2, -3, 'venta');$tpl$,
    true
) ON CONFLICT (name) DO NOTHING;
