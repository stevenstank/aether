-- schema
CREATE TABLE employees (id INT PRIMARY KEY, name TEXT NOT NULL, dept TEXT, salary FLOAT, email TEXT UNIQUE, manager INT);
CREATE INDEX emp_dept ON employees (dept);
CREATE INDEX emp_salary ON employees (salary);
INSERT INTO employees VALUES
  (1, 'Grace', 'eng', 185000, 'grace@corp.example', NULL),
  (2, 'Alan', 'eng', 150000.5, 'alan@corp.example', 1),
  (3, 'Ada', 'research', 172000, 'ada@corp.example', 1),
  (4, 'Linus', 'eng', 99000, 'linus@corp.example', 2),
  (5, 'Barbara', 'research', 134500, NULL, 3),
  (6, 'Ken', 'ops', 120000, 'ken@corp.example', 1),
  (7, 'Margaret', 'ops', 143250.75, 'margaret@corp.example', 6),
  (8, 'Dennis', 'eng', NULL, 'dennis@corp.example', 2);
SELECT * FROM employees;
SELECT name, salary FROM employees WHERE dept = 'eng' ORDER BY salary DESC;
EXPLAIN SELECT name FROM employees WHERE dept = 'eng';
EXPLAIN SELECT name FROM employees WHERE id = 3 AND dept = 'eng';
EXPLAIN SELECT name FROM employees WHERE 140000 < salary;
EXPLAIN SELECT name FROM employees WHERE name LIKE 'A%';
SELECT name FROM employees WHERE salary > 140000 AND salary <= 180000 ORDER BY name;
SELECT name, UPPER(dept) AS d, LENGTH(name) AS n FROM employees WHERE name LIKE '%a%' ORDER BY n DESC, name LIMIT 3;
SELECT dept, COUNT(*) AS n, SUM(salary), ROUND(AVG(salary), 2) AS avg, MIN(name), MAX(salary) FROM employees GROUP BY dept ORDER BY dept;
SELECT dept, COUNT(salary) FROM employees GROUP BY dept HAVING COUNT(*) > 2;
SELECT COUNT(*), COUNT(DISTINCT dept), COUNT(email) FROM employees;
SELECT name FROM employees WHERE email IS NULL OR salary IS NULL;
SELECT name, COALESCE(email, 'n/a') AS contact FROM employees WHERE manager IN (1, 3) ORDER BY 1;
SELECT name FROM employees WHERE salary BETWEEN 100000 AND 150000 ORDER BY salary;
SELECT name || ' <' || email || '>' AS who FROM employees WHERE id NOT IN (1, 2, 3, 4, 5, 6) ORDER BY who;
SELECT id, salary * 1.1 AS raised, salary / 0 FROM employees WHERE id = 1;
SELECT id % 3 AS bucket, COUNT(*) FROM employees GROUP BY id % 3 ORDER BY bucket;
SELECT SUBSTR(name, 2, 3), ABS(-id), -id FROM employees WHERE id < 3;
SELECT name FROM employees ORDER BY salary LIMIT 2 OFFSET 1;
-- constraints
INSERT INTO employees VALUES (9, 'Dup', 'eng', 1, 'grace@corp.example', NULL);
INSERT INTO employees VALUES (1, 'DupKey', 'eng', 1, 'x@y', NULL);
INSERT INTO employees VALUES (10, NULL, 'eng', 1, 'z@y', NULL);
INSERT INTO employees VALUES (11, 'Typed', 'eng', 'lots', 'q@y', NULL);
INSERT INTO employees (id, name) VALUES (12, 'Partial');
INSERT INTO employees VALUES (13, 'Short');
SELECT COUNT(*) FROM employees;
-- statement atomicity: second row violates uniqueness, first must not stay
INSERT INTO employees (id, name, email) VALUES (20, 'A', 'a@x'), (21, 'B', 'a@x');
SELECT COUNT(*) FROM employees WHERE id >= 20;
UPDATE employees SET email = 'same@x' WHERE dept = 'ops';
SELECT name, email FROM employees WHERE dept = 'ops';
-- transactions
BEGIN;
UPDATE employees SET salary = salary + 1000 WHERE dept = 'eng';
DELETE FROM employees WHERE dept = 'ops';
SELECT dept, COUNT(*) FROM employees GROUP BY dept ORDER BY dept;
BEGIN;
INSERT INTO employees (id, name, dept) VALUES (30, 'Temp', 'tmp');
UPDATE employees SET dept = 'tmp' WHERE id = 3;
SELECT name FROM employees WHERE dept = 'tmp' ORDER BY name;
ROLLBACK;
SELECT name FROM employees WHERE dept = 'tmp';
COMMIT;
SELECT name, salary FROM employees WHERE dept = 'eng' ORDER BY id;
BEGIN;
DELETE FROM employees;
SELECT COUNT(*) FROM employees;
ROLLBACK;
SELECT COUNT(*) FROM employees;
ROLLBACK;
COMMIT;
BEGIN;
CREATE TABLE nope (x INT);
ROLLBACK;
CHECKPOINT;
SELECT name FROM employees WHERE dept = 'research' ORDER BY name;
EXPLAIN SELECT * FROM employees WHERE dept = 'research';
SHOW TABLES;
SELECT nosuch FROM employees;
SELECT * FROM ghosts;
SELECT MAX(*) FROM employees;
SELECT UNKNOWNFN(1) FROM employees;
DROP TABLE employees;
SHOW TABLES;
