package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type workerChannel struct {
	upperSend chan<-byte //halo for worker above is sent
	upperRec <-chan byte //halo above is received from worker
	lowerSend chan<-byte //halo for worker below is sent
	lowerRec <-chan byte //halo below is received from worker

	parityBit <-chan bool //tells workers when to output the world
}

func outputWorld(p golParams, d distributorChans, world [][]byte, turns int) {
	d.io.command <- ioOutput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight) + "-" + strconv.Itoa(turns)}, "x")

	// Sends the world to the pgm channel
	for y := range world {
		for x := range world[y] {
			d.io.outputVal <- world[y][x]
		}
	}
}

func modPos(d, m int) int {
	var res = d % m
	if (res < 0 && m > 0) || (res > 0 && m < 0) {
		return res + m
	}
	return res
}

func worker(startY, endY int, p golParams, out chan<- byte, in <-chan byte, wc workerChannel, nextTurn chan bool) {

	smallWorldHeight := endY-startY+2

	//Initialise small world as well as temp small world
	smallWorld := make([][]byte, smallWorldHeight)
	tempSmallWorld := make([][]byte, smallWorldHeight)

	for i := range smallWorld {
		smallWorld[i] = make([]byte, p.imageWidth)
		tempSmallWorld[i] = make([]byte, p.imageWidth)
	}

	//Creates new small world with halos
	for y := 0; y < smallWorldHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			smallWorld[y][x] = <- in
		}
	}

	for turn:= 0; turn < p.turns; turn++ {

		synchroniser := true
		for synchroniser{
			select{
			case _ = <-wc.parityBit:
				//If parity bit is received (signalling that image needs to be sent back), each image is sent back once
				//each worker has finished their turn, before starting the new one
				for y := 1; y < endY-startY+1; y++ {
					for x := 0; x < p.imageWidth; x++ {
						out <- smallWorld[y][x]
					}
				}
				//Otherwise, worker waits for a signal to proceed onto the next turn from the distributor and sets
				//synchroniser to false to break out of for loop to start new turn
			case _ = <-nextTurn:
				synchroniser = false
			}
		}
		//At beginning of each turn, must send and receive halos
		//Sending halos
		for halo := 0; halo < p.imageWidth; halo++ {
			wc.upperSend <- smallWorld[1][halo]
			wc.lowerSend <- smallWorld[smallWorldHeight-2][halo]
		}

		//Receiving halos
		for halo := 0; halo < p.imageWidth; halo++ {
			smallWorld[0][halo] = <- wc.upperRec
			smallWorld[smallWorldHeight-1][halo] = <- wc.lowerRec
		}

		//Counts number of alive neighbours for each cell
		for y := 1; y < endY-startY+1; y++ {
			for x := 0; x < p.imageWidth; x++ {
				alive := 0
				//Calculates how many alive cells there are by adding up all alive cells (each alive cell is 255)
				//and then dividing it by 255 to get the number of alive cells
				alive = int(smallWorld[modPos(y-1 ,smallWorldHeight)][modPos(x-1 ,p.imageWidth)]) + int(smallWorld[modPos(y-1, smallWorldHeight)][modPos(x, p.imageWidth)]) + int(smallWorld[modPos(y-1, smallWorldHeight)][modPos(x+1, p.imageWidth)]) +
					int(smallWorld[modPos(y, smallWorldHeight)][modPos(x-1, p.imageWidth)])                        +                              int(smallWorld[(y) % smallWorldHeight][(x+1) % p.imageWidth])           +
					int(smallWorld[modPos(y+1, smallWorldHeight)][modPos(x-1, p.imageWidth)]) +     int(smallWorld[(y+1) % smallWorldHeight][(x) % p.imageWidth])     + int(smallWorld[(y+1) % smallWorldHeight][(x+1) % p.imageWidth])
				alive = alive/255

				//Flips cell or sends back original if no change was made
				if smallWorld[y][x] != 0 {
					if alive < 2 || alive > 3 {
						tempSmallWorld[y][x] = smallWorld[y][x] ^ 0xFF
					} else {
						tempSmallWorld[y][x] = smallWorld[y][x]
					}
				} else {
					if alive == 3 {
						tempSmallWorld[y][x] = smallWorld[y][x] ^ 0xFF
					} else {
						tempSmallWorld[y][x] = smallWorld[y][x]
					}
				}
			}
		}

		//Copy temp world into real world
		for i := range smallWorld {
			smallWorld[i] = make([]byte, len(tempSmallWorld[i]))
			copy(smallWorld[i], tempSmallWorld[i])
		}
	}

	//Sends the world back once all turns are finished
	for y := 1; y < endY-startY+1; y++ {
		for x := 0; x < p.imageWidth; x++ {
			out <- smallWorld[y][x]
		}
	}

}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p golParams, d distributorChans, alive chan []cell, keyChan <-chan rune) {

	// Create the 2D slice to store the world.
	world := make([][]byte, p.imageHeight)
	for i := range world {
		world[i] = make([]byte, p.imageWidth)
	}

	// Request the io goroutine to read in the image with the given filename.
	d.io.command <- ioInput
	d.io.filename <- strings.Join([]string{strconv.Itoa(p.imageWidth), strconv.Itoa(p.imageHeight)}, "x")

	// The io goroutine sends the requested image byte by byte, in rows.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			val := <-d.io.inputVal
			if val != 0 {
				fmt.Println("Alive cell at", x, y)
				world[y][x] = val
			}
		}
	}

	//Height the worker will work on
	workerHeight := p.imageHeight / p.threads
	workerHeightRemainder := 0

	//Calculates the remainder that will be added onto the final worker
	workerHeightRemainder = p.imageHeight % p.threads

	//Channels to send halos to and from worker
	sendChans := make([]chan<- byte, 2 * p.threads)
	recChans := make([]<-chan byte, 2 * p.threads)
	parityBitChans := make([]chan bool, p.threads)
	nextTurns := make([]chan bool, p.threads)

	//Array of channels intended for workers
	out := make([]chan byte, p.threads)
	in := make([]chan byte, p.threads)

	//Create receive and send channel
	for i := range sendChans {
		halo := make(chan byte, 2 * p.imageWidth)
		recChans[i] = halo
		sendChans[i] = halo
	}

	//Create in, out and parity bit channels
	for i := range out {
		out[i] = make(chan byte, p.imageWidth)
		in[i] = make(chan byte, p.imageWidth)
		parityBitChans[i] = make(chan bool, p.threads)
		nextTurns[i] = make(chan bool)
	}

	//For each worker assign correct channels (upper send halo of one worker is lower receive halo of another for example)
	for t := 0; t < p.threads-1; t++ {
		var wc workerChannel
		wc.upperSend = sendChans[t*2]
		wc.lowerSend = sendChans[(t*2)+1]
		wc.upperRec = recChans[modPos((t*2)-1, 2*p.threads)]
		wc.lowerRec = recChans[modPos((t+1)*2, 2*p.threads)]
		wc.parityBit = parityBitChans[t]

		go worker(t*workerHeight, (t+1)*workerHeight, p, out[t], in[t], wc, nextTurns[t])

		for y := 0; y < workerHeight+2; y++ {
			for x := 0; x < p.imageWidth; x++ {
				in[t] <- world[modPos(y+(t*(workerHeight)-1), p.imageHeight)][x]
			}
		}
	}
	//Spare worker that takes in the remainder if channels are not a power of 2
	var wc2 workerChannel
	wc2.upperSend = sendChans[(p.threads-1)*2]
	wc2.lowerSend = sendChans[((p.threads-1)*2)+1]
	wc2.upperRec = recChans[modPos(((p.threads-1)*2)-1, 2*p.threads)]
	wc2.lowerRec = recChans[modPos(((p.threads-1)+1)*2, 2*p.threads)]
	wc2.parityBit = parityBitChans[p.threads-1]

	go worker((p.threads-1)*workerHeight, ((p.threads)*workerHeight)+workerHeightRemainder, p, out[p.threads-1], in[p.threads-1], wc2, nextTurns[p.threads-1])

	for y := 0; y < workerHeight+workerHeightRemainder+2; y++ {
		for x := 0; x < p.imageWidth; x++ {
			in[p.threads-1] <- world[modPos(y+((p.threads-1)*(workerHeight)-1), p.imageHeight)][x]
		}
	}

	turns := 0
	timeAfter := time.NewTicker(time.Second * 2)

	// Calculate the new state of Game of Life after the given number of turns.
	for turns = 0; turns < p.turns; turns++ {
		//Event handler select statement
		select {
		case <-timeAfter.C: //After 2 seconds
			//Notify the workers to synchronise
			for _, parityBitChan := range parityBitChans {
				parityBitChan <- true
			}

			alive := 0
			//Calculate how many alive cells there are and then divide by 255
			for t := 0; t < p.threads-1; t++ {
				for y := 0; y < workerHeight; y++ {
					for x := 0; x < p.imageWidth; x++ {
						alive += int(<-out[t])
					}
				}
			}
			for y := 0; y < workerHeight+workerHeightRemainder; y++ {
				for x := 0; x < p.imageWidth; x++ {
					alive += int(<-out[p.threads-1])
				}
			}
			alive = alive / 255
			fmt.Println("Alive cells: " + strconv.Itoa(alive))

		case i := <-keyChan:
			if i == 's' {
				//Notify the workers to synchronise
				for _, parityBitChan := range parityBitChans {
					parityBitChan <- true
				}

				for t := 0; t < p.threads-1; t++ {
					for y := 0; y < workerHeight; y++ {
						for x := 0; x < p.imageWidth; x++ {
							world[y+(t*workerHeight)][x] = <- out[t]
						}
					}
				}

				for y := 0; y < workerHeight+workerHeightRemainder; y++ {
					for x := 0; x < p.imageWidth; x++ {
						world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
					}
				}
				//Output the world in its current turn
				outputWorld(p, d, world, turns)

			} else if i == 'p' {
				fmt.Println("Game paused, current turn: ", strconv.Itoa(turns))

				//Infinite for loop until p is pressed again (busy waiting not sure if best solution)
				for x := true; x == true; {
					select {
					case resume := <-keyChan:
						if resume == 'p' {
							fmt.Println("Continuing.")
							x = false
						}
					}
				}

			} else if i == 'q' {
				//Notify the workers to synchronise
				for _, parityBitChan := range parityBitChans {
					parityBitChan <- true
				}

				for t := 0; t < p.threads-1; t++ {
					for y := 0; y < workerHeight; y++ {
						for x := 0; x < p.imageWidth; x++ {
							world[y+(t*workerHeight)][x] =<- out[t]
						}
					}
				}

				for y := 0; y < workerHeight+workerHeightRemainder; y++ {
					for x := 0; x < p.imageWidth; x++ {
						world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
					}
				}
				//Output world and quit game
				outputWorld(p, d, world, turns)
				StopControlServer()
				os.Exit(0)
			}
			//Default needed as otherwise game cannot proceed until button is pressed
			default:
		}
		//Each worker is sent a true bool to notify it that it can proceed with the following turn
		for j := 0; j < p.threads; j++{
			nextTurns[j] <- true
		}
	}

	//Workers send back their worlds after all turns are finished
	for t := 0; t < p.threads-1; t++ {
		for y := 0; y < workerHeight; y++ {
			for x := 0; x < p.imageWidth; x++ {
				world[y+(t*workerHeight)][x] = <- out[t]
			}
		}
	}
	//Spare worker
	for y := 0; y < workerHeight+workerHeightRemainder; y++ {
		for x := 0; x < p.imageWidth; x++ {
			world[y+((p.threads-1)*workerHeight)][x] = <-out[p.threads-1]
		}
	}
	//World output after all turns
	outputWorld(p, d, world, p.turns)

	// Create an empty slice to store coordinates of cells that are still alive after p.turns are done.
	var finalAlive []cell
	// Go through the world and append the cells that are still alive.
	for y := 0; y < p.imageHeight; y++ {
		for x := 0; x < p.imageWidth; x++ {
			if world[y][x] != 0 {
				finalAlive = append(finalAlive, cell{x: x, y: y})
			}
		}
	}

	// Make sure that the Io has finished any output before exiting.
	d.io.command <- ioCheckIdle
	<-d.io.idle

	// Return the coordinates of cells that are still alive.
	alive <- finalAlive
}