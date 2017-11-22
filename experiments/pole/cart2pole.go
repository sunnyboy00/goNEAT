package pole

import (
	"github.com/yaricom/goNEAT/experiments"
	"github.com/yaricom/goNEAT/neat/network"
	"fmt"
	"github.com/yaricom/goNEAT/neat"
	"math"
	"github.com/yaricom/goNEAT/neat/genetics"
	"os"
	"sort"
)

const thirty_six_degrees = 36 * math.Pi / 180.0


// The double pole-balancing experiment both Markov and non-Markov versions
type CartDoublePoleGenerationEvaluator struct {
	// The output path to store execution results
	OutputPath string
	// The flag to indicate whether to apply Markov evaluation variant
	Markov     bool

	// The flag to indicate whether to use continuous activation or discrete
	ActionType experiments.ActionType
}

// The structure to describe cart pole emulation
type CartPole struct {
	// The maximal fitness
	maxFitness         float64
	// The flag to indicate whether to apply Markovian evaluation variant
	isMarkov           bool
	// Flag that we are looking at the champ
	nonMarkovLong      bool
	// Flag we are testing champ's generalization
	generalizationTest bool
	// The state of the system (x, x acceleration, thetha 1, thetha 1 acceleration, thetha 2, thetha 2 acceleration)
	state              [6]float64

	jiggleStep         [1000]float64

	length2            float64
	massPole2          float64
	minInc             float64
	poleInc            float64
	massInc            float64

	// Queues used for Gruau's fitness which damps oscillations
	balanced_sum       int
	cartpos_sum        float64
	cartv_sum          float64
	polepos_sum        float64
	polev_sum          float64
}

// Perform evaluation of one epoch on double pole balancing
func (ex CartDoublePoleGenerationEvaluator) GenerationEvaluate(pop *genetics.Population, generation *experiments.Generation, context *neat.NeatContext) (err error) {
	cartPole := newCartPole(ex.Markov)

	cartPole.nonMarkovLong = false
	cartPole.generalizationTest = false

	// Evaluate each organism on a test
	for _, org := range pop.Organisms {
		res := ex.orgEvaluate(org, cartPole)

		if res {
			// This will be winner in Markov case
			generation.Solved = true
			generation.WinnerNodes = len(org.Genotype.Nodes)
			generation.WinnerGenes = org.Genotype.Extrons()
			generation.WinnerEvals = context.PopSize * generation.Id + org.Genotype.Id
			generation.Best = org
			break // we have winner
		}
	}

	// Check for winner in Non-Markov case
	if !ex.Markov {
		// Sort the species by max organism fitness in descending order - the highest fitness first
		sorted_species := make([]*genetics.Species, len(pop.Species))
		copy(sorted_species, pop.Species)
		sort.Sort(sort.Reverse(genetics.ByOrganismFitness(sorted_species)))

		// First update what is checked and unchecked
		var curr_species *genetics.Species
		for _, curr_species = range sorted_species {
			if max, _ := curr_species.ComputeMaxAndAvgFitness(); max > curr_species.MaxFitnessEver {
				curr_species.IsChecked = false
			}
		}

		// Now find first (most fit) species that is unchecked
		curr_species = nil
		for _, curr_species = range sorted_species {
			if !curr_species.IsChecked {
				break
			}
		}
		if curr_species == nil {
			curr_species = sorted_species[0]
		}

		// Remember it was checked
		curr_species.IsChecked = true

		// the organism champion
		champion := curr_species.FindChampion()
		champion_fitness := champion.Fitness

		// Now check to make sure the champion can do 100000 evaluations
		cartPole.nonMarkovLong = true
		cartPole.generalizationTest = false

		if ex.orgEvaluate(champion, cartPole) {
			// the champion passed non-Markov long test
			cartPole.nonMarkovLong = false
			// Given that the champ passed, now run it on generalization tests
			state_vals := [5]float64{0.05, 0.25, 0.5, 0.75, 0.95}
			score := 0
			for s0c := 0; s0c <= 4; s0c++ {
				for s1c := 0; s1c <= 4; s1c++ {
					for s2c := 0; s2c <= 4; s2c++ {
						for s3c := 0; s3c <= 4; s3c++ {
							cartPole.state[0] = state_vals[s0c] * 4.32 - 2.16
							cartPole.state[1] = state_vals[s1c] * 2.70 - 1.35
							cartPole.state[2] = state_vals[s2c] * 0.12566304 - 0.06283152 // 0.06283152 = 3.6 degrees
							cartPole.state[3] = state_vals[s3c] * 0.30019504 - 0.15009752 // 0.15009752 = 8.6 degrees
							cartPole.state[4] = 0.0
							cartPole.state[5] = 0.0

							cartPole.generalizationTest = true

							// The champion needs to be flushed here because it may have
							// leftover activation from its last test run that could affect
							// its recurrent memory
							generation.Best.Phenotype.Flush()

							if ex.orgEvaluate(champion, cartPole) {
								score++
							}
						}
					}
				}
			}

			if score >= 200 {
				// The generalization test winner
				neat.DebugLog(fmt.Sprintf("The non-Markov champion found! (generalization = %d)", score))
				generation.Solved = true
				generation.WinnerNodes = len(champion.Genotype.Nodes)
				generation.WinnerGenes = champion.Genotype.Extrons()
				generation.WinnerEvals = context.PopSize * generation.Id + champion.Genotype.Id
				generation.Best = champion
			} else {
				neat.DebugLog("The non-Markov champion failed to generalize in non-Markov test")
				generation.Best.Fitness = champion_fitness; // Restore the champ's fitness
			}
		} else {
			neat.DebugLog("The non-Markov champion failed the 100,000 run in non-Markov test")
			champion.Fitness = champion_fitness; // Restore the champ's fitness
		}
	}


	// Fill statistics about current epoch
	generation.FillPopulationStatistics(pop)

	// Only print to file every print_every generations
	if generation.Solved || generation.Id % context.PrintEvery == 0 {
		pop_path := fmt.Sprintf("%s/gen_%d", ex.OutputPath, generation.Id)
		file, err := os.Create(pop_path)
		if err != nil {
			neat.ErrorLog(fmt.Sprintf("Failed to dump population, reason: %s\n", err))
		} else {
			pop.WriteBySpecies(file)
		}
	}

	if generation.Solved {
		// print winner organism
		for _, org := range pop.Organisms {
			if org.IsWinner {
				// Prints the winner organism to file!
				org_path := fmt.Sprintf("%s/%s", ex.OutputPath, "pole2_winner")
				file, err := os.Create(org_path)
				if err != nil {
					neat.ErrorLog(fmt.Sprintf("Failed to dump winner organism genome, reason: %s\n", err))
				} else {
					org.Genotype.Write(file)
					neat.InfoLog(fmt.Sprintf("Generation #%d winner %d dumped to: %s\n", generation.Id, org.Genotype.Id, org_path))
				}
				break
			}
		}
	} else {
		// Move to the next epoch if failed to find winner
		neat.DebugLog(">>>>> start next generation")
		_, err = pop.Epoch(generation.Id + 1, context)
	}

	return err
}

// This methods evaluates provided organism for cart double pole-balancing task
func (ex *CartDoublePoleGenerationEvaluator) orgEvaluate(organism *genetics.Organism, cartPole *CartPole) bool {
	// Try to balance a pole now
	organism.Fitness = cartPole.evalNet(organism.Phenotype, ex.ActionType)

	if neat.LogLevel == neat.LogLevelDebug {
		neat.DebugLog(fmt.Sprintf("Organism #%3d\tfitness: %f", organism.Genotype.Id, organism.Fitness))
	}

	// DEBUG CHECK if organism is damaged
	if !(cartPole.nonMarkovLong && cartPole.generalizationTest) && organism.CheckChampionChildDamaged() {
		neat.WarnLog(fmt.Sprintf("ORGANISM DAMAGED:\n%s", organism.Genotype))
	}

	// Decide if its a winner, in Markov Case
	if cartPole.isMarkov {
		if organism.Fitness >= cartPole.maxFitness {
			organism.IsWinner = true
		}
	} else if cartPole.nonMarkovLong {
		// if doing the long test non-markov
		if organism.Fitness >= 99999 {
			organism.IsWinner = true
		}
	} else if cartPole.generalizationTest {
		if organism.Fitness >= 999 {
			organism.IsWinner = true
		}
	} else {
		organism.IsWinner = false
	}
	return organism.IsWinner
}


// If markov is false, then velocity information will be withheld from the network population (non-Markov)
func newCartPole(markov bool) *CartPole {
	return &CartPole{
		maxFitness: 100000,
		isMarkov: markov,
		minInc: 0.001,
		poleInc: 0.05,
		massInc: 0.01,
		length2: 0.05,
		massPole2: 0.01,
	}
}

func (cp *CartPole)evalNet(net *network.Network, actionType experiments.ActionType) (steps float64) {
	non_markov_max := 1000.0
	if cp.nonMarkovLong {
		non_markov_max = 100000.0
	}

	input := make([]float64, 7)

	cp.resetState()

	steps = 1

	if cp.isMarkov {
		for ; steps < cp.maxFitness; steps++ {
			input[0] = (cp.state[0] + 2.4) / 4.8
			input[1] = (cp.state[1] + 1.0) / 2.0
			input[2] = (cp.state[2] + thirty_six_degrees) / (thirty_six_degrees * 2.0)//0.52
			input[3] = (cp.state[3] + 1.0) / 2.0
			input[4] = (cp.state[4] + thirty_six_degrees) / (thirty_six_degrees * 2.0)//0.52
			input[5] = (cp.state[5] + 1.0) / 2.0
			input[6] = 0.5

			net.LoadSensors(input)

			/*-- activate the network based on the input --*/
			if res, err := net.Activate(); !res {
				//If it loops, exit returning only fitness of 1 step
				neat.DebugLog(fmt.Sprintf("Failed to activate Network, reason: %s", err))
				return 1.0
			}
			action := net.Outputs[0].Activation
			if actionType == experiments.DiscreteAction {
				// make action values discrete
				if action < 0.5 {
					action = 0
				} else {
					action = 1
				}
			}
			cp.performAction(action, steps)

			if cp.outsideBounds() {
				// if failure stop it now
				break;
			}

			//fmt.Printf("x: % f, xv: % f, t1: % f, t2: % f, angle: % f\n", cp.state[0], cp.state[1], cp.state[2], cp.state[4], thirty_six_degrees)
		}
		return steps
	} else {
		// The non Markov case
		for ; steps < non_markov_max; steps++ {
			input[0] = (cp.state[0] + 2.4) / 4.8
			input[1] = (cp.state[2] + thirty_six_degrees) / (thirty_six_degrees * 2.0)//0.52
			input[2] = (cp.state[4] + thirty_six_degrees) / (thirty_six_degrees * 2.0)//0.52
			input[3] = 0.5

			net.LoadSensors(input)

			/*-- activate the network based on the input --*/
			if res, err := net.Activate(); !res {
				//If it loops, exit returning only fitness of 1 step
				neat.DebugLog(fmt.Sprintf("Failed to activate Network, reason: %s", err))
				return 0.0001
			}

			action := net.Outputs[0].Activation
			if action < 0.5 {
				action = 0
			} else {
				action = 1
			}
			cp.performAction(action, steps)
			if actionType == experiments.DiscreteAction {
				// make action values discrete
				if action < 0.5 {
					action = 0
				} else {
					action = 1
				}
			}
			if cp.outsideBounds() {
				// if failure stop it now
				break;
			}
		}
		/*-- If we are generalizing we just need to balance it a while --*/
		if cp.generalizationTest {
			return float64(cp.balanced_sum)
		}

		// Sum last 100
		jiggle_total := 0.0
		if steps > 100.0 && !cp.nonMarkovLong {
			// Adjust for array bounds and count
			for count := int(steps - 99.0 - 2.0); count <= int(steps - 2.0); count++ {
				jiggle_total += cp.jiggleStep[count]
			}
		}
		if !cp.nonMarkovLong {
			var non_markov_fitness float64
			if cp.balanced_sum > 100 {
				non_markov_fitness = 0.1 * float64(cp.balanced_sum) / 1000.0 + 0.9 * 0.75 / float64(jiggle_total)
			} else {
				non_markov_fitness = 0.1 * float64(cp.balanced_sum) / 1000.0
			}
			if neat.LogLevel == neat.LogLevelDebug {
				neat.DebugLog(fmt.Sprintf("Balanced: %d jiggle: %d ***\n", cp.balanced_sum, jiggle_total))
			}
			return non_markov_fitness
		} else {
			return steps
		}
	}
}

func (cp *CartPole) performAction(action, step_num float64) {
	const TAU = 0.001

	/*--- Apply action to the simulated cart-pole ---*/
	// Runge-Kutta 4th order integration method
	var dydx[6]float64
	for i := 0; i < 2; i++ {
		dydx[0] = cp.state[1]
		dydx[2] = cp.state[3]
		dydx[4] = cp.state[5]
		cp.step(action, cp.state, &dydx)
		cp.rk4(action, cp.state, dydx, &cp.state, TAU)
	}

	// Record this state
	cp.cartpos_sum += math.Abs(cp.state[0])
	cp.cartv_sum += math.Abs(cp.state[1]);
	cp.polepos_sum += math.Abs(cp.state[2]);
	cp.polev_sum += math.Abs(cp.state[3]);

	if step_num < 1000 {
		cp.jiggleStep[int(step_num) - 1] = math.Abs(cp.state[0]) + math.Abs(cp.state[1]) + math.Abs(cp.state[2]) + math.Abs(cp.state[3])
	}
	if !cp.outsideBounds() {
		cp.balanced_sum++
	}
}

func (cp *CartPole) step(action float64, st[6]float64, derivs *[6]float64) {
	const MUP = 0.000002
	const GRAVITY = -9.8
	const MASSCART = 1.0
	const MASSPOLE_1 = 0.5
	const LENGTH_1 = 0.5 // actually half the pole's length
	const FORCE_MAG = 10.0

	force := (action - 0.5) * FORCE_MAG * 2.0
	cos_theta_1 := math.Cos(st[2])
	sin_theta_1 := math.Sin(st[2])
	g_sin_theta_1 := GRAVITY * sin_theta_1
	cos_theta_2 := math.Cos(st[4])
	sin_theta_2 := math.Sin(st[4])
	g_sin_theta_2 := GRAVITY * sin_theta_2

	ml_1 := LENGTH_1 * MASSPOLE_1
	ml_2 := cp.length2 * cp.massPole2
	temp_1 := MUP * st[3] / ml_1
	temp_2 := MUP * st[5] / ml_2
	fi_1 := (ml_1 * st[3] * st[3] * sin_theta_1) + (0.75 * MASSPOLE_1 * cos_theta_1 * (temp_1 + g_sin_theta_1))
	fi_2 := (ml_2 * st[5] * st[5] * sin_theta_2) + (0.75 * cp.massPole2 * cos_theta_2 * (temp_2 + g_sin_theta_2))
	mi_1 := MASSPOLE_1 * (1 - (0.75 * cos_theta_1 * cos_theta_1))
	mi_2 := cp.massPole2 * (1 - (0.75 * cos_theta_2 * cos_theta_2))

	//fmt.Printf("%f -> %f\n", action, force)

	derivs[1] = (force + fi_1 + fi_2) / (mi_1 + mi_2 + MASSCART)
	derivs[3] = -0.75 * (derivs[1] * cos_theta_1 + g_sin_theta_1 + temp_1) / LENGTH_1
	derivs[5] = -0.75 * (derivs[1] * cos_theta_2 + g_sin_theta_2 + temp_2) / cp.length2
}

func (cp *CartPole) rk4(f float64, y, dydx [6]float64, yout *[6]float64, tau float64) {
	var yt, dym, dyt [6]float64
	hh := tau * 0.5
	h6 := tau / 6.0
	for i := 0; i <= 5; i++ {
		yt[i] = y[i] + hh * dydx[i]
	}
	cp.step(f, yt, &dyt)

	dyt[0] = yt[1]
	dyt[2] = yt[3]
	dyt[4] = yt[5]
	for i := 0; i <= 5; i++ {
		yt[i] = y[i] + hh * dyt[i]
	}
	cp.step(f, yt, &dym)

	dym[0] = yt[1]
	dym[2] = yt[3]
	dym[4] = yt[5]
	for i := 0; i <= 5; i++ {
		yt[i] = y[i] + tau * dym[i]
		dym[i] += dyt[i]
	}
	cp.step(f, yt, &dyt)

	dyt[0] = yt[1]
	dyt[2] = yt[3]
	dyt[4] = yt[5]
	for i := 0; i <= 5; i++ {
		yout[i] = y[i] + h6 * (dydx[i] + dyt[i] + 2.0 * dym[i])
	}
}

// Check if simulation goes outside of bounds
func (cp *CartPole) outsideBounds() bool {
	const failureAngle = thirty_six_degrees

	return cp.state[0] < -2.4 ||
		cp.state[0] > 2.4 ||
		cp.state[2] < -failureAngle ||
		cp.state[2] > failureAngle ||
		cp.state[4] < -failureAngle ||
		cp.state[4] > failureAngle
}

func (cp *CartPole)resetState() {
	if cp.isMarkov {
		// Clear all fitness records
		cp.cartpos_sum = 0.0
		cp.cartv_sum = 0.0
		cp.polepos_sum = 0.0
		cp.polev_sum = 0.0

		cp.state[0], cp.state[1], cp.state[2], cp.state[3], cp.state[4], cp.state[5] = 0, 0, 0, 0, 0, 0
	}
	cp.balanced_sum = 0 // Always count # balanced
	if cp.generalizationTest {
		cp.state[0], cp.state[1], cp.state[3], cp.state[4], cp.state[5] = 0, 0, 0, 0, 0
		cp.state[2] = math.Pi / 180.0 // one_degree
	} else {
		cp.state[4], cp.state[5] = 0, 0
	}
}


