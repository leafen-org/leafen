// The box each built-in icon's ink fills, as Notion's icon of that name
// inks it (its 885 icons rastered at 400px, 2026-11-18 — the box the ink
// above half opacity covers, each side to a sub-pixel): each side as half
// steps (EDGE_STEP, a 64th of the box) in from the box's side — left, top,
// right, bottom — then the icons that ink it. A side is a whole number of
// half steps but where Notion's ink stops off its grid (a hexagon's points,
// a pentagon's, a triangle's cut corners, a sum's sigma). Each glyph is brought into it
// (nativeIconGlyphFit.ts placeInBox), its lines as heavy as before.

import { EDGE_STEP } from "./nativeIconGlyphShapes";
import type { Box } from "./nativeIconGlyphPath";

const BOXES = /* screen words: not shown */ `
  8 8 8 8: accessibility add alarm alert aries arrow-circle-down arrow-down arrow-left arrow-right arrow-up
    baby-bottle barricade baseball bone branch-create branch-fork branch-merge broccoli browser-stop bus-metro button
    cancer cd chat chat-user checkmark chicken circle circle-alternate circle-arrows-horizontal circle-arrows-vertical
    circle-dashed circle-dot circle-five-eighths circle-four-eighths circle-one-eighth circle-remove
    circle-seven-eighths circle-six-eighths circle-three-eighths circle-two-eighths city clear clock clock-alternate
    clothes-button clover clover-four-leaf coffee color-picker comment compass contrast copy crayon darks dashboard
    dialogue duster emoji emoji-angry emoji-big-sad emoji-disappointed emoji-grinning emoji-grinning-smiling-eyes
    emoji-heart-eyes emoji-neutral emoji-sad emoji-smiling-eyes emoji-sunglasses emoji-surprised emoji-winking error
    expand fabric-swatch facial-tissues filtered first-aid football frying-pan garlic gemini globe grapes hair-care
    headphones help-alternate inbox info-alternate key lights link location looped-square meat menstrual-pad midtones
    moon mushroom navigation no no-entry octagon onion orbit oven parking parking-no peace peanut pen pencil
    phone-call pill pisces playback-play-button poo pot pregnancy-test private public reference rename report
    safety-pin search seed send set-square shop smoking smoking-no steering-wheel sticker stroller subtitles
    suit-heart sword takeout-box target taurus token transfers tree user-circle user-circle-dashed user-circle-filled
    username wrench yin-yang zoom-in zoom-out
  10 10 10 10: checkmark-square clipping code-scan column die1 die2 die3 die4 die5 die6 grid grid-dense jar news plus
    promoted row server square square-alternate square-dashed square-one-fourth square-three-fourths
    square-two-fourths
  10 8 10 8: bell book-closed bread capricorn categories chemistry dental downward gradebook grave hail hashtag
    highball hurricane magnet palm-tree rain shogi sleet storm traffic-light upward user
  8 10 8 10: activity archive backward boot chart-area chart-line command-line feed folder forward grid-wide-six
    heart-rate-monitor move movie-clapboard movie-clapboard-play new-folder slide sync sync-reverse table
    upload-folder
  12 8 12 8: chess-rook coaster compressed-document cut database document flash hourglass laundry-dryer laundry-washer
    lock lock-keyhole metronome move-document new-document passport signature-document tablet toilet tornado
    upload-document
  8 6 8 8: banana bus bus-double-decker cash-register chess-king connecting-flight fire gift golf immigration
    invitation mirror power slideshow slideshow-play suit-club suit-spade
  6 6 6 6: archery bell-off camera-off computer-chip crop directions gear git leo microphone-off pizza sun verified
    volume-off
  6 8 6 8: cherries confetti-ball fireworks knife-kitchen lungs script shell skirt stars telephone virgo wheat
  14 8 14 8: badge card-club card-diamond card-heart card-spade color-swatch fragile question-mark tooth tulip wine
  16 8 16 8: baby bag beer bookmark chevrons-vertical exclamation-mark-double hanafuda mahjong medication phone
    squeeze-tube
  6 6 6 8: chess-queen network print thinking thought thought-alert thought-dialogue volcano
  8 4.32 8 4.32: hexagon hexagon-alternate hexagon-dashed hexagon-five-sixths hexagon-four-sixths hexagon-one-sixth
    hexagon-three-sixths hexagon-two-sixths
  8 8 8 10: briefcase eject first-aid-kit handbag libra roller-skate suitcase traffic-cone
  8 10 8 8: cocktail computer defibrillator heart heart-box-bow heart-rate heartbroken tulip-name-tag
  5 4.61 5 8: pentagon pentagon-alternate pentagon-dashed pentagon-four-fifths pentagon-one-fifth
    pentagon-three-fifths pentagon-two-fifths
  6 10 6 10: binoculars junk laundry-basket pretzel timeline toilet-paper torii
  8 6 6 8: bullseye carrot compose create phone-speaker screwdriver wheelchair-motorized
  4 8 4 8: bathtub chili-pepper conceal judicial-scales trophy tshirt
  5.44 8 5.47 10: triangle triangle-alternate triangle-dashed triangle-one-third triangle-two-thirds warning
  6 12 6 12: hairdryer list-indent microwave photo-landscape ticket-admission window
  4 12 4 12: aquarius butterfly laptop map view
  8 2 8 10: castle cigarette government mandir school
  8 4 8 8: baggage-claim leaf-monstera rubber-stamp shopping-bag snare-drum
  8 6 8 6: boat flood package sailboat suit-diamond
  8 6 8 10: burger earthquake home library video-game-joystick
  8 14 8 14: card credit-card grid-wide postcard stairs
  10 4 10 8: christmas-tree conifer-tree paint-brush-wide share symbol
  12 6 12 6: apron bluetooth ear layers stovetop
  0 10 0 10: elevator paifang suspension-bridge temple
  4 6 4 6: cloud-off shower video-camera-off view-off
  4 8 4 10: bug castle-japanese chart-mixed inline-skate
  4 14 4 14: cassette gym videotape voicemail
  6 4 6 4: bunk-bed fleur-de-lis spider upload
  6 14 6 10: ambulance delivery-truck preview tv
  6 14 6 14: barcode boarding-pass cash ticket
  8 6 8 12: calendar calendar-day calendar-month calendar-week
  8 6 10 6: flag flag-checkered flag-pennant flag-swallowtail
  8 8 4 8: coffee-maker perfume pram sock
  12 6 12 8: chess-bishop laundry-detergent pump-bottle strawberry
  3 4 3 8: coat shirt suit
  4 4 4 10: broom-and-dustpan puzzle sink
  4 6 4 8: ghost hare subtask
  4 10 4 10: camping-tent chart long-bone
  4 10 4 14: cloud cloud-no cloud-yes
  4 16 0 16: battery battery-charged battery-charging
  6 4 6 8: airplane champagne-bottle delete
  6 6 6 10: art hanger taxi
  6 8 6 10: bowl-food factory shopping-basket
  6 8 6 12: bra chair sunset
  12 4 12 8: cupcake hot-air-balloon lemon
  13 8 13 8: dna egg postage-stamp
  14 6 14 6: electric-plug light-bulb paper-towels
  14 7 14 8: person-feminine person-masculine vitruvian-man
  14 14 14 14: playback-pause playback-play playback-stop
  16 4 16 4: dress refrigerator watch-analog
  16 8 12 8: brain profile throat
  0 8 0 8: conversation dining
  0 8 0 10: partly-cloudy-day partly-cloudy-night
  0 10 0 14: bugle trumpet
  0 11 0 12: bicycle motorcycle
  2 8 2 8: currency-coin language
  2 8 2 12: scooter teapot
  2 10 2 10: snippet towel
  2 14 2 14: candy mouth
  2 23 2 19: glasses sunglasses
  3 8 3 8: long-sleeve-shirt mobile
  4 2 4 10: church mosque
  4 8 2 8: bomb pitcher
  4 8 2 10: fuel snorkel
  4 10 4 8: cafe elephant
  4 10 6 8: donkey vinyl-record
  4 12 4 16: clothes-iron friends
  4 16 4 12: arch-bridge tortoise
  4 16 4 16: check keyboard-alternate
  5 5 5 5: new-alert new-badge
  5 5 5.28 8: star-half star-outline
  6 4 4 8: soap soda-bottle
  6 6 8 8: avocado customs
  6 6 12 6: music note-sixteenth-beamed
  6 10 8 10: arrow-right-line color-palette
  6 12 6 8: nut pin
  6 12 6 10: book car
  6 16 6 16: cloudy scrub-brush
  8 4 4 8: sponge wine-bottle
  8 5 8 5: science sliders-horizontal
  8 7.81 8 8: arrow-up-basic priority-high
  8 8 7.87 8: arrow-left-basic arrow-right-basic
  8 8 8 6: t-square tampon
  8 8 8 7.81: arrow-down-basic priority-low
  8 8 10 6: feather leaf
  8 8.13 8.13 8: chart-donut chart-pie
  8 10 10 8: megaphone sagittarius
  8 12 8 12: list mail
  8 13 8 13: description reorder
  8 18 8 18: playback-next playback-previous
  10 0 10 10: refresh refresh-reverse
  10 6 10 8: arrow-down-line save
  10 8 8 8: vacuum-cleaner wind
  10 8 8 10: tabs tabs-user
  10 8 10 6: arrow-up-line pants
  10 18 10 18: playback-fast-forward playback-rewind
  11 6 11 6: calculator orange
  12 2 12 8: unlock unlock-keyhole
  12 8 12 6: shield theatre
  12 8.26 12 8: chess-pawn game-pawn
  14 4 14 8: backpack daisy
  15 8 15 8: bookmark-outline receipt
  16 0 16 0: electric-guitar guitar
  17 8 27 8: note-half note-quarter
  18 6 18 8: dish-soap map-pin
  18 8 18 8: champagne tropical-cocktail
  0 2 0 4: dance
  0 4 2 10: food-and-drink
  0 8 6 8: history
  0 10 0 8: currency
  0 10 2 8: dog
  0 10 2 14: reply-all
  0 12 0 12: ringed-planet
  0 12 0 16: groups
  0 12 0 20: rainbow
  0 14 0 14: whistle
  0 14 0 16: meeting
  0 14 2 12: helicopter
  0 16 0 16: atm
  0 18 0 12: mountains
  0 22 0 20: clarinet
  0 22 0 22.11: skateboard
  1 7 1 8: people
  1 10 1 10: alien-pixel
  1.82 14 1.82 14: arrows-horizontal
  2 2 6 6: lost-and-found
  2 2.18 0 2: temperature-warm
  2 2.18 3.2 2: temperature-cool
  2 4 2 6: makeup-brush
  2 4 2 8: forest-fire
  2 4 6 8: luggage-cart
  2 6 2 6: paint-bucket
  2 6 2 8: crab
  2 6 4 8: whale
  2 7 1 8: service-counter
  2 7 2 12: sharing
  2 8 0 8: code
  2 8 2 6: bee
  2 8 2 10: sheep
  2 8 2 14: escalator
  2 8 3 10: graduate
  2 8 6 8: shopping-cart
  2 8 6 10: shovel-and-pail
  2 8 12 8: directional-sign-left
  2 10 0 14: bounce
  2 10 4 10: sunscreen
  2 12 0 12: belt
  2 12 1 16: following
  2 12 2 12: couch
  2 12 3 16: follow
  2 12 4 16: unfollow
  2 14 8 12: lounge
  2 16 2 10: pig
  2 16 2 16: fish
  2 17 2 17: infinity
  2.4 15.68 2.43 15.68: 123
  2.82 14.3 2.78 14.53: numero
  3 5.82 3 8: branch
  3.65 14.24 3.58 14.24: font
  3.81 16.13 4 8: departures
  4 2 4 2: bathtub-shower
  4 2 6 8: gavel
  4 2 8 8: bowling
  4 4 2 10: mop-and-bucket
  4 4 4 4: brightness-high
  4 4 4 8: construction-crane
  4 4 6 6: soy
  4 4.51 4.48 4: gears
  4 6 2 10: grocery
  4 6 4 10: synagogue
  4 6 6 6: log-in
  4 6 8 10: playlist
  4 6.11 8 8.58: checklist
  4 8 1.79 2: scorpio
  4 8 4 12: boombox
  4 8 4.22 8: skip-forward
  4 8 6 8: flatware
  4 8 6 10: snake
  4 8 8 4: syringe
  4 8 8 10: paint-brush
  4 8 10 8: sign-in
  4 10 2 10: cow
  4 12 4 10: gem
  4 12 4 12.45: wifi
  4 12.26 4 8: cellular
  4 13 0 13: drafts
  4 15 4 15: video-camera
  4 16 0 12: wrapping-paper
  4 16 4 10: fire-truck
  4 16 4 18: plate-food
  4 19 4 23: phone-end-call
  4 20 4 20: shoe
  4.22 8 4 8: skip-backward
  5 4.58 5 8: menorah
  5 5.28 5 5.28: arrows-swap-vertically
  5 7 6 8: litter-disposal
  5 8 5 8: sliders-vertical
  5 8 6 10: movie-camera
  5 10 6 10: dependency
  5 12 5 12: keyboard
  5.28 5 5.28 5: arrows-swap-horizontally
  5.38 5.41 5.38 5.41: square-circle
  5.76 5 5.89 9.28: star
  6 0 0 6: magic-wand
  6 2 2 6: flashlight
  6 2 6 4: kite
  6 2 6 10: robot
  6 4 4 6: confetti-party-popper
  6 4 6 1.82: download
  6 4 6 10: jack-o-lantern
  6 4 8 8: cards
  6 5 5.28 5: shuffle
  6 6 1.82 6: log-out
  6 6 6 5: cursor-click
  6 6 6.11 6: condense
  6 6 8 6: ear-hearing-aid
  6 7 4 8: translate
  6 8 4 4: blood-pressure
  6 8 4 6: hammer
  6 8 4 8: pull-request
  6 8 4.16 10: kind
  6 8 6 16: video-game-classic
  6 8 8 6: poultry
  6 8 8 8: stomach
  6 8.26 6 12: sunrise
  6 10 2 10: music-album
  6 10 4 8: wheelchair
  6 10 4 14: redirect
  6 10 8 14: reply
  6 11 6 11: movie
  6 12 2 18: baseball-cap
  6 12 4 12: pump
  6 12 6 7: cursor-button
  6 13 6 13: wall
  6 14 6 12: bed
  6 16 6 14: underwear
  6 18 2 10: camping-trailer
  6 20 6 16: bowl
  6.11 8 4 8: directional-sign
  6.34 4 6.27 4: helm
  6.82 10 8 8: arrivals
  7 4 6 8: cat
  7 6 6 7: crutch
  7 8 7 10: shorts
  7 8.35 7 8.35: swap-vertically
  7 10 7 8: heart-outline
  7 13 7 13: sandwich
  7.26 6 8 14: undo
  7.33 5 7.33 5: repeat
  7.81 8 10 8: arrow-turn-left
  8 0 8 8: drink
  8 0 8 12: radio
  8 2 8 8: telescope
  8 3.23 16 8: geography
  8 4 6 8: beer-bottle
  8 5 8 6: umbrella
  8 6 6 6: stethoscope
  8 6 6.24 8: notification
  8 6 6.34 0: iterate
  8 6 7.33 14: redo
  8 6 8 4: headset
  8 6 11 8: duck
  8 7.52 8 8.48: piano
  8 8 2.18 8: fan-deck
  8 8 4 10: skull-profile
  8 8 4.29 8: music-artist
  8 8 6 8: ice-skate
  8 8 8 7.2: mathematics
  8 8 8 9: soccer
  8 8 8 12: camera
  8 8 12 4: official-document
  8 8.77 8 8.77: exit
  8 10 6 10: arrow-left-line
  8 10 6 12: camera-roll
  8 10 6 14: send-to
  8 12.29 8 12.22: volume-high
  8 14 8 15.81: checkmark-line
  8 16 8 12: video-game
  8 18 8 8: truck
  8 20 8 20: priority-mid
  8 26 8 26: more
  8.13 8.13 8 8.13: basketball
  8.26 8 8.22 8: star-of-life
  8.35 7 8.35 7: swap-horizontally
  8.42 8 8.35 8: snowflake
  9 6 9 8: cactus
  9 9 9 9: burst
  9.79 8 9.79 8: asterisk
  9.89 8 10.34 6: key-antique
  10 0 12 0.22: violin
  10 2 10 6: ornament
  10 2 12 8: chess-knight
  10 4 10 4: chart-alternate
  10 4 10 10: apple
  10 4 12 8: microscope
  10 6 4 8: stopwatch
  10 6 6.24 8: bell-notification
  10 6 9 10: thumbs-up
  10 6 10 6: alien
  10 6 10 10: paste
  10 6 18 8: fire-extinguisher
  10 8 1.82 8: sign-out
  10 8 4 8: directional-sign-right
  10 8 7.81 8: arrow-turn-right
  10 8 8 6: paint-roller
  10 8 8 7.84: attachment
  10 8 10 12: fog
  10 10 4 10: extension
  10 10 6 6: tag
  10 10 9 6: thumbs-down
  10 10 10 2: scarf
  10 12 10 12: tumbler
  10 29 10 29: remove
  11 7 12 8: run
  11 8 11 4: train-high-speed
  11 8 11 10: skull
  12 2 12 2: corn
  12 2 12 4: train-light-rail
  12 2.11 12 10: cake
  12 3 12 4: monorail
  12 4 12 4: anchor
  12 6 10 8: camera-roll-portrait
  12 8 6 8: toaster
  12 8 12 4: train-metro
  12 12 12 12: close
  13 2 13 1: bikini
  13 5 13 8: train-magnetic-levitation
  13 5.82 13 8: merge
  13 6 13 4: train
  13 6 13 8: water
  13 10 13 10: keypad
  13 13 15 15: arrow-southeast
  13 15 15 13: arrow-northeast
  14 1.82 14 1.82: arrows-vertical
  14 2 14 2: luggage
  14 2 14 8: potted-plant
  14 4 14 4: balloon
  14 4 14 10: broom
  14 6 14 8: microphone
  14 8 13 8: hand
  14 8 16 8: fork-and-knife
  14 16 14 16: note-whole
  15 0 15 8: soft-serve
  15 13 13 15: arrow-southwest
  15 15 13 13: arrow-northwest
  16 4 16 2.11: rocket
  16 4 16 6: pear
  16 6 16 8: map-pin-alternate
  16 6 18 8: spray-bottle
  16 7 10 8: wheelchair-access
  16 7 14 8: walk
  16 8 16 4: push-pin
  16 8 16 8.13: child
  17 4 17 8: dairy
  17 6 17 6: do-not-disturb
  17 8 17 8: note-eighth
  17 8.54 17 8: note-sixteenth
  17.22 10.59 17.18 10.59: formula
  18.66 4 11.33 10: mop
  20 2 20 8: lipstick
  20 6 18 6: comb
  20 8 16 8: cursor
  22 4 22 4: ruler
  23 8 23 8: fork
  23 8 29 8: knife
  24 6 24 2: necktie
  24 8 24 8: spoon
  26 8 26 8: exclamation-mark
`;

export const NATIVE_ICON_BOXES: ReadonlyMap<string, Box> = new Map(
  BOXES.split(/\n(?= {2}\d)/).filter((group) => group.includes(":")).flatMap((group) => {
    const [sides, names] = group.split(":") as [string, string];
    const [left, top, right, bottom] = sides.trim().split(" ").map(Number) as [number, number, number, number];
    const box: Box = [left * EDGE_STEP, top * EDGE_STEP, 20 - right * EDGE_STEP, 20 - bottom * EDGE_STEP];
    return names.trim().split(/\s+/).map((name): [string, Box] => [name, box]);
  }),
);

