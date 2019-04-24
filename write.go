package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	outputFile, outputError := os.OpenFile("log.log", os.O_WRONLY|os.O_CREATE, 0666)
	if outputError != nil {
		fmt.Printf("An error occurred with file opening or creation\n")
		return
	}
	defer outputFile.Close()

	outputWriter := bufio.NewWriter(outputFile)

	for i := 0; ; i++ {

		// Create a little message to send to clients,
		// including the current time.
		outputString := fmt.Sprintf("%d - the time is %v\n", i, time.Now())
		outputWriter.WriteString(outputString)
		outputWriter.Flush()
		// Print a nice log message and sleep for 5s.
		log.Printf("Writer message %d ", i)
		time.Sleep(0.5e9)

	}

}
