package actors_test

import (
	"bytes"
	"context"
	"math/rand"
	"testing"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/aerrors"
	"github.com/filecoin-project/lotus/chain/address"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/sectorbuilder"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func TestMinerCommitSectors(t *testing.T) {
	var worker, client address.Address
	var minerAddr address.Address
	opts := []HarnessOpt{
		HarnessAddr(&worker, 1000000),
		HarnessAddr(&client, 1000000),
		HarnessActor(&minerAddr, &worker, actors.StorageMinerCodeCid,
			func() cbg.CBORMarshaler {
				return &actors.StorageMinerConstructorParams{
					Owner:      worker,
					Worker:     worker,
					SectorSize: 1024,
					PeerID:     "fakepeerid",
				}
			}),
	}

	h := NewHarness(t, opts...)
	h.vm.Syscalls.ValidatePoRep = func(ctx context.Context, maddr address.Address, ssize uint64, commD, commR, ticket, proof, seed []byte, sectorID uint64) (bool, aerrors.ActorError) {
		// all proofs are valid
		return true, nil
	}

	ret, _ := h.SendFunds(t, worker, minerAddr, types.NewInt(100000))
	ApplyOK(t, ret)

	ret, _ = h.InvokeWithValue(t, client, actors.StorageMarketAddress, actors.SMAMethods.AddBalance, types.NewInt(2000), nil)
	ApplyOK(t, ret)

	s := sectorbuilder.UserBytesForSectorSize(1024)
	deal := h.makeFakeDeal(t, minerAddr, worker, client, s)
	ret, _ = h.Invoke(t, worker, actors.StorageMarketAddress, actors.SMAMethods.PublishStorageDeals,
		&actors.PublishStorageDealsParams{
			Deals: []actors.StorageDeal{*deal},
		})
	ApplyOK(t, ret)
	var dealIds actors.PublishStorageDealResponse
	if err := dealIds.UnmarshalCBOR(bytes.NewReader(ret.Return)); err != nil {
		t.Fatal(err)
	}

	dealid := dealIds.DealIDs[0]

	ret, _ = h.Invoke(t, worker, minerAddr, actors.MAMethods.PreCommitSector,
		&actors.SectorPreCommitInfo{
			SectorNumber: 1,
			CommR:        []byte("cats"),
			SealEpoch:    10,
			DealIDs:      []uint64{dealid},
		})
	ApplyOK(t, ret)

	h.vm.SetBlockHeight(100)
	ret, _ = h.Invoke(t, worker, minerAddr, actors.MAMethods.ProveCommitSector,
		&actors.SectorProveCommitInfo{
			Proof:    []byte("prooofy"),
			SectorID: 1,
			DealIDs:  []uint64{dealid}, // TODO: weird that i have to pass this again
		})
	ApplyOK(t, ret)
}

func (h *Harness) makeFakeDeal(t *testing.T, miner, worker, client address.Address, size uint64) *actors.StorageDeal {
	data := make([]byte, size)
	rand.Read(data)
	commP, err := sectorbuilder.GeneratePieceCommitment(bytes.NewReader(data), size)
	if err != nil {
		t.Fatal(err)
	}

	prop := actors.StorageDealProposal{
		PieceRef:  commP[:],
		PieceSize: size,
		//PieceSerialization SerializationMode // Needs to be here as it tells how data in the sector maps to PieceRef cid

		Client:   client,
		Provider: miner,

		ProposalExpiration: 10000,
		Duration:           150,

		StoragePricePerEpoch: types.NewInt(1),
		StorageCollateral:    types.NewInt(0),

		//ProposerSignature *types.Signature
	}

	if err := api.SignWith(context.TODO(), h.w.Sign, client, &prop); err != nil {
		t.Fatal(err)
	}

	storageDeal := actors.StorageDeal{
		Proposal: prop,
	}
	if err := api.SignWith(context.TODO(), h.w.Sign, worker, &storageDeal); err != nil {
		t.Fatal(err)
	}

	return &storageDeal
}
