CREATE TABLE orders (id INTEGER PRIMARY KEY, region TEXT, status TEXT, amount REAL);
INSERT INTO orders VALUES
 (1,'east','paid',100.0),(2,'east','void',999.0),(3,'west','paid',50.5),
 (4,'west','paid',25.0),(5,'north','void',10.0),(6,'north','paid',75.25);
