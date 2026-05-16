package twentyq

// seeds is the seed pool. The LLM derives category + initial hint per round
// so no metadata table is needed. Keep entries:
//   - single concrete nouns (no abstractions, no proper nouns, no plurals)
//   - distinguishable to a player with general knowledge
//   - ASCII-only so prompt-engineered locale traps don't bias the model
//
// Pool size matters because random selection is uniform without exclusion —
// at N=50 a power user sees a repeat within ~9 plays (birthday paradox); at
// N=200 the same threshold moves to ~17. This list is deliberately wide
// across categories to reduce thematic clustering when the model picks hints.
var seeds = []string{
	// instruments
	"guitar", "piano", "drum", "violin", "flute", "trumpet", "organ", "harmonica",
	"saxophone", "cello", "accordion", "banjo", "xylophone", "clarinet", "tambourine",
	"harp", "ukulele", "bagpipes",
	// animals — land
	"elephant", "kangaroo", "tiger", "horse", "snake", "owl", "wolf", "rabbit",
	"giraffe", "hedgehog", "rhinoceros", "panda", "cheetah", "gorilla", "raccoon",
	"squirrel", "platypus", "bat", "armadillo", "sloth",
	// animals — sea + air
	"dolphin", "eagle", "octopus", "penguin", "shark", "whale", "starfish", "crab",
	"jellyfish", "lobster", "parrot", "hummingbird", "flamingo", "ostrich", "seahorse",
	// food
	"pizza", "sushi", "burger", "ramen", "taco", "pho", "curry", "salad",
	"chocolate", "cheese", "pancake", "waffle", "donut", "croissant", "dumpling",
	"sandwich", "lasagna", "popcorn", "kimchi", "biryani",
	// fruits + vegetables
	"banana", "pineapple", "watermelon", "strawberry", "mango", "avocado", "coconut",
	"pomegranate", "broccoli", "carrot", "cucumber", "pumpkin", "garlic", "onion",
	// vehicles
	"bicycle", "car", "airplane", "boat", "train", "motorcycle", "helicopter",
	"submarine", "tractor", "skateboard", "scooter", "rocket", "ambulance", "bulldozer",
	// sports + games
	"soccer", "basketball", "tennis", "swimming", "boxing", "golf", "chess", "skiing",
	"badminton", "cricket", "rugby", "bowling", "surfing", "archery", "fencing",
	// household items
	"refrigerator", "microwave", "vacuum", "toaster", "kettle", "blender", "lamp",
	"sofa", "mirror", "broom", "umbrella", "scissors", "telephone", "clock",
	"hammer", "screwdriver", "ladder", "candle", "flashlight",
	// nature + places
	"mountain", "volcano", "desert", "glacier", "waterfall", "island", "forest",
	"beach", "cave", "river",
	// space
	"telescope", "asteroid", "comet", "satellite", "galaxy",
	// occupations
	"firefighter", "astronaut", "chef", "carpenter", "librarian", "dentist",
	"photographer", "scientist", "lifeguard", "magician",
	// tools + tech
	"camera", "compass", "binoculars", "keyboard", "headphones",
	"microscope", "printer", "stapler",
	// clothing
	"sweater", "sneakers", "scarf", "helmet", "gloves", "raincoat",
	// drinks
	"coffee", "tea", "lemonade", "smoothie",
}
