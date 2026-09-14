package client

// DrawRepaint is true while repainting between client ticks. Drawing may rebuild
// rendering caches, but must not spawn, move, or destroy simulation objects.
// The client loop owns this flag; legacy.SetDrawRepaint also sets its C counterpart.
var DrawRepaint bool
