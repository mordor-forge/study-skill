CC = gcc
CFLAGS = -Wall -Wextra -Wpedantic -std=c17 -g
SRC = $(wildcard src/*.c)
OBJ = $(SRC:.c=.o)
TARGET = study

.PHONY: all clean run

all: $(TARGET)

$(TARGET): $(OBJ)
	$(CC) $(CFLAGS) -o $@ $^

%.o: %.c
	$(CC) $(CFLAGS) -c -o $@ $<

clean:
	rm -f $(TARGET) $(OBJ)

run: all
	./$(TARGET)
