package twentyq

// seeds is the JS-parity seed list. Add nouns here — the LLM derives category
// + initial hint per round so no metadata table is needed.
var seeds = []string{
	// instruments
	"guitar", "piano", "drum", "violin", "flute", "trumpet", "organ", "harmonica",
	// animals
	"elephant", "dolphin", "eagle", "kangaroo", "octopus", "penguin", "tiger", "horse", "snake", "owl",
	// food
	"pizza", "sushi", "burger", "ramen", "taco", "pho", "curry", "salad", "chocolate", "cheese",
	// vehicles
	"bicycle", "car", "airplane", "boat", "train", "motorcycle", "helicopter", "submarine",
	// sports
	"soccer", "basketball", "tennis", "swimming", "boxing", "golf", "chess", "skiing",
	// household items
	"refrigerator", "microwave", "vacuum", "toaster", "kettle", "blender", "lamp", "sofa", "mirror", "broom",
}
